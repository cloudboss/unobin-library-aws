package iam

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	sdk "github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

const samlTestARN = "arn:aws:iam::123456789012:saml-provider/Example"

func TestSAMLProviderCreate(t *testing.T) {
	f := newFakeIAM(t)
	f.on("CreateSAMLProvider", func(_ int, form url.Values) (int, string) {
		require.Equal(t, url.Values{
			"Action": {"CreateSAMLProvider"}, "Version": {"2010-05-08"},
			"Name": {"Example"}, "SAMLMetadataDocument": {strings.Repeat("x", 1000)},
		}, form)
		return 200, `<CreateSAMLProviderResponse><CreateSAMLProviderResult>` +
			`<SAMLProviderArn>` + samlTestARN + `</SAMLProviderArn>` +
			`</CreateSAMLProviderResult></CreateSAMLProviderResponse>`
	})
	f.on("GetSAMLProvider", func(_ int, form url.Values) (int, string) {
		require.Equal(t, url.Values{"Action": {"GetSAMLProvider"},
			"Version": {"2010-05-08"}, "SAMLProviderArn": {samlTestARN}}, form)
		return 200, `<GetSAMLProviderResponse><GetSAMLProviderResult>` +
			`<SAMLMetadataDocument>remote</SAMLMetadataDocument><SAMLProviderUUID>uuid</SAMLProviderUUID>` +
			`<ValidUntil>2030-01-02T03:04:05Z</ValidUntil>` +
			`<Tags><member><Key>owner</Key><Value>test</Value></member>` +
			`<member><Key>aws:system</Key><Value>ignore</Value></member></Tags>` +
			`</GetSAMLProviderResult></GetSAMLProviderResponse>`
	})
	r := &SAMLProviderResource{Name: "Example", SAMLMetadataDocument: strings.Repeat("x", 1000)}
	out, err := r.Create(t.Context(), f.configuration())
	require.NoError(t, err)
	until := "2030-01-02T03:04:05Z"
	require.Equal(t, &SAMLProviderResourceOutput{Arn: samlTestARN, Name: "Example",
		SAMLMetadataDocument: "remote", SAMLProviderUUID: "uuid", ValidUntil: &until,
		Tags: map[string]string{"owner": "test"}}, out)
}

func samlResult(action, fields string) string {
	return "<" + action + "Response><" + action + "Result>" + fields +
		"</" + action + "Result></" + action + "Response>"
}

func samlError(code string) (int, string) {
	return 400, `<ErrorResponse><Error><Code>` + code + `</Code>` +
		`<Message>test failure</Message></Error></ErrorResponse>`
}

func samlActions(f *fakeIAM) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, request := range f.requests {
		out = append(out, request.Get("Action"))
	}
	return out
}

func samlRequest(action string) url.Values {
	return url.Values{"Action": {action}, "Version": {"2010-05-08"},
		"SAMLProviderArn": {samlTestARN}}
}

func TestSAMLProviderTaggedCreateFallback(t *testing.T) {
	for _, tc := range []struct {
		name, region, code string
		fallback           bool
	}{
		{"tagged", "us-east-1", "", false},
		{"standard", "us-east-1", "AccessDenied", false},
		{"iso", "us-iso-east-1", "AccessDenied", true},
		{"unclassified", "us-iso-east-1", "InvalidInput", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeIAM(t)
			metadata := strings.Repeat("raw & < > %20\n", 100)
			f.on("CreateSAMLProvider", func(n int, form url.Values) (int, string) {
				want := url.Values{"Action": {"CreateSAMLProvider"}, "Version": {"2010-05-08"},
					"Name": {"Example"}, "SAMLMetadataDocument": {metadata}}
				if n == 1 {
					want.Set("Tags.member.1.Key", "owner")
					want.Set("Tags.member.1.Value", "")
				}
				require.Equal(t, want, form)
				if n == 1 && tc.code != "" {
					return samlError(tc.code)
				}
				return 200, samlResult("CreateSAMLProvider",
					"<SAMLProviderArn>"+samlTestARN+"</SAMLProviderArn>")
			})
			f.on("TagSAMLProvider", func(_ int, form url.Values) (int, string) {
				want := samlRequest("TagSAMLProvider")
				want.Set("Tags.member.1.Key", "owner")
				want.Set("Tags.member.1.Value", "")
				require.Equal(t, want, form)
				return 200, samlResult("TagSAMLProvider", "")
			})
			f.on("GetSAMLProvider", func(_ int, form url.Values) (int, string) {
				require.Equal(t, samlRequest("GetSAMLProvider"), form)
				return 200, samlResult("GetSAMLProvider", "")
			})
			cfg := f.configuration()
			cfg.Region = &tc.region
			tags := map[string]string{"owner": "", "aws:ignored": "system"}
			r := SAMLProviderResource{Name: "Example", SAMLMetadataDocument: metadata, Tags: &tags}
			out, err := r.Create(t.Context(), cfg)
			switch {
			case tc.fallback:
				require.NoError(t, err)
				require.Equal(t, []string{"CreateSAMLProvider", "CreateSAMLProvider",
					"TagSAMLProvider", "GetSAMLProvider"}, samlActions(f))
			case tc.code == "":
				require.NoError(t, err)
				require.Equal(t, []string{"CreateSAMLProvider", "GetSAMLProvider"}, samlActions(f))
			default:
				require.ErrorContains(t, err, tc.code)
				require.Nil(t, out)
				require.Equal(t, []string{"CreateSAMLProvider"}, samlActions(f))
			}
		})
	}
}

func TestSAMLProviderUpdateObservation(t *testing.T) {
	for _, mode := range []string{"drift", "saved-output", "tags-only", "computed-only",
		"changed-desired", "remove-all"} {
		t.Run(mode, func(t *testing.T) {
			f := newFakeIAM(t)
			metadata := strings.Repeat("x", 1000)
			tags := map[string]string{"keep": "1", "change": "new", "add": ""}
			r := SAMLProviderResource{Name: "NewDesiredName", SAMLMetadataDocument: metadata, Tags: &tags}
			observed := &SAMLProviderResourceOutput{Arn: samlTestARN,
				SAMLMetadataDocument: metadata, Tags: map[string]string{
					"keep": "1", "change": "old", "remove": "yes", "aws:x": "system"}}
			if mode == "drift" || mode == "saved-output" {
				observed.SAMLMetadataDocument = "remote drift"
			}
			if mode == "computed-only" {
				observed.Tags = tags
			}
			if mode == "remove-all" {
				r.Tags = nil
			}
			prior := runtime.Prior[SAMLProviderResource, *SAMLProviderResourceOutput, *awsCfg]{
				Inputs: r, Outputs: observed, Observed: observed,
			}
			if mode == "saved-output" {
				prior.Observed = nil
			}
			if mode == "changed-desired" {
				prior.Inputs.SAMLMetadataDocument = "previous desired"
			}
			for _, action := range []string{"UntagSAMLProvider", "TagSAMLProvider",
				"UpdateSAMLProvider", "GetSAMLProvider"} {
				f.on(action, func(_ int, form url.Values) (int, string) {
					want := samlRequest(action)
					switch action {
					case "UntagSAMLProvider":
						keys := []string{"remove"}
						if mode == "remove-all" {
							keys = []string{"change", "keep", "remove"}
						}
						for i, key := range keys {
							want.Set(fmt.Sprintf("TagKeys.member.%d", i+1), key)
						}
					case "TagSAMLProvider":
						got := map[string]string{}
						for i := 1; i <= 2; i++ {
							prefix := fmt.Sprintf("Tags.member.%d.", i)
							got[form.Get(prefix+"Key")] = form.Get(prefix + "Value")
							want.Set(prefix+"Key", form.Get(prefix+"Key"))
							want.Set(prefix+"Value", form.Get(prefix+"Value"))
						}
						require.Equal(t, map[string]string{"change": "new", "add": ""}, got)
					case "UpdateSAMLProvider":
						want.Set("SAMLMetadataDocument", metadata)
					}
					require.Equal(t, want, form)
					return 200, samlResult(action, "")
				})
			}
			out, err := r.Update(t.Context(), f.configuration(), prior)
			require.NoError(t, err)
			require.Equal(t, samlTestARN, out.Arn)
			require.Nil(t, out.ValidUntil)
			want := []string{"UntagSAMLProvider", "TagSAMLProvider"}
			if mode == "computed-only" {
				want = nil
			}
			if mode == "remove-all" {
				want = []string{"UntagSAMLProvider"}
			}
			if mode == "drift" || mode == "saved-output" || mode == "changed-desired" {
				want = append(want, "UpdateSAMLProvider")
			}
			require.Equal(t, append(want, "GetSAMLProvider"), samlActions(f))
		})
	}
}

func TestSAMLProviderReadDeleteErrors(t *testing.T) {
	for _, code := range []string{"", "NoSuchEntity", "AccessDenied", "DeleteConflict"} {
		t.Run(code, func(t *testing.T) {
			f := newFakeIAM(t)
			for _, action := range []string{"GetSAMLProvider", "DeleteSAMLProvider"} {
				f.on(action, func(_ int, form url.Values) (int, string) {
					require.Equal(t, samlRequest(action), form)
					if code != "" {
						return samlError(code)
					}
					return 200, samlResult(action, "")
				})
			}
			r := SAMLProviderResource{Name: "unrelated desired name"}
			prior := runtime.Prior[SAMLProviderResource, *SAMLProviderResourceOutput, *awsCfg]{
				Outputs: &SAMLProviderResourceOutput{Arn: samlTestARN},
			}
			cfg := f.configuration()
			out, err := r.Read(t.Context(), cfg, prior)
			switch code {
			case "":
				require.NoError(t, err)
				require.Nil(t, out.ValidUntil)
			case "NoSuchEntity":
				require.ErrorIs(t, err, runtime.ErrNotFound)
			default:
				require.ErrorContains(t, err, code)
				require.NotErrorIs(t, err, runtime.ErrNotFound)
			}
			err = r.Delete(t.Context(), cfg, prior)
			if code == "" || code == "NoSuchEntity" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, code)
			}
			require.Equal(t, []string{"GetSAMLProvider", "DeleteSAMLProvider"}, samlActions(f))
		})
	}
}

func TestSAMLProviderMalformedIdentity(t *testing.T) {
	f := newFakeIAM(t)
	cfg := f.configuration()
	r := SAMLProviderResource{}
	prior := runtime.Prior[SAMLProviderResource, *SAMLProviderResourceOutput, *awsCfg]{
		Outputs: &SAMLProviderResourceOutput{Arn: "malformed"},
	}
	_, err := r.Read(t.Context(), cfg, prior)
	require.ErrorContains(t, err, "parse SAML provider ARN")
	_, err = r.Update(t.Context(), cfg, prior)
	require.ErrorContains(t, err, "parse SAML provider ARN")
	require.ErrorContains(t, r.Delete(t.Context(), cfg, prior), "parse SAML provider ARN")
	require.Empty(t, samlActions(f))
	request, err := r.prepareSAMLProvider("arn:aws:iam::123:saml-provider/a/b")
	require.NoError(t, err)
	require.Equal(t, "a/b", request.name)
}

func TestSAMLProviderTagPagination(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			f := newFakeIAM(t)
			f.on("ListSAMLProviderTags", func(n int, form url.Values) (int, string) {
				want := samlRequest("ListSAMLProviderTags")
				if n == 2 {
					want.Set("Marker", "next")
				}
				require.Equal(t, want, form)
				if n == 2 && fail {
					return samlError("AccessDenied")
				}
				if n == 1 {
					return 200, samlResult("ListSAMLProviderTags", `<IsTruncated>true</IsTruncated>`+
						`<Marker>next</Marker><Tags><member><Key>first</Key><Value>1</Value></member>`+
						`<member><Key>aws:x</Key><Value>system</Value></member></Tags>`)
				}
				return 200, samlResult("ListSAMLProviderTags", `<IsTruncated>false</IsTruncated>`+
					`<Tags><member><Key>second</Key><Value></Value></member></Tags>`)
			})
			client, err := newClient(t.Context(), f.configuration())
			require.NoError(t, err)
			got, err := listSAMLProviderTags(t.Context(), client, samlTestARN)
			if fail {
				require.ErrorContains(t, err, "AccessDenied")
			} else {
				require.NoError(t, err)
				require.Equal(t, map[string]string{"first": "1", "second": ""}, got)
			}
		})
	}
}

type samlReadStub struct {
	output *sdk.GetSAMLProviderOutput
	err    error
}

func (s samlReadStub) GetSAMLProvider(context.Context, *sdk.GetSAMLProviderInput,
	...func(*sdk.Options)) (*sdk.GetSAMLProviderOutput, error) {
	return s.output, s.err
}

func TestSAMLProviderNilGet(t *testing.T) {
	for _, created := range []bool{false, true} {
		_, err := readSAMLProvider(t.Context(), samlReadStub{}, samlTestARN, created)
		require.Error(t, err)
		if created {
			require.NotErrorIs(t, err, runtime.ErrNotFound)
		} else {
			require.ErrorIs(t, err, runtime.ErrNotFound)
		}
	}
}

type samlDeleteStub struct{ t *testing.T }

func (s samlDeleteStub) DeleteSAMLProvider(ctx context.Context, in *sdk.DeleteSAMLProviderInput,
	_ ...func(*sdk.Options)) (*sdk.DeleteSAMLProviderOutput, error) {
	require.NoError(s.t, ctx.Err())
	deadline, ok := ctx.Deadline()
	require.True(s.t, ok)
	require.WithinDuration(s.t, time.Now().Add(2*time.Minute), deadline, time.Second)
	require.Equal(s.t, samlTestARN, *in.SAMLProviderArn)
	return nil, errors.New("cleanup error")
}

func TestSAMLProviderCleanupContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	original := errors.New("original")
	err := cleanupSAMLProvider(ctx, samlDeleteStub{t}, samlTestARN, original)
	require.ErrorIs(t, err, original)
	require.ErrorContains(t, err, "cleanup error")
}

func TestSAMLProviderInitializationErrors(t *testing.T) {
	for _, mode := range []string{"no-arn", "missing", "malformed-response"} {
		t.Run(mode, func(t *testing.T) {
			f := newFakeIAM(t)
			f.on("CreateSAMLProvider", func(_ int, _ url.Values) (int, string) {
				if mode == "no-arn" {
					return 200, samlResult("CreateSAMLProvider", "")
				}
				return 200, samlResult("CreateSAMLProvider",
					"<SAMLProviderArn>"+samlTestARN+"</SAMLProviderArn>")
			})
			f.on("GetSAMLProvider", func(_ int, _ url.Values) (int, string) {
				if mode == "missing" {
					return samlError("NoSuchEntity")
				}
				return 200, "<GetSAMLProviderResponse>broken"
			})
			f.on("DeleteSAMLProvider", func(_ int, form url.Values) (int, string) {
				require.Equal(t, samlRequest("DeleteSAMLProvider"), form)
				return 200, samlResult("DeleteSAMLProvider", "")
			})
			r := SAMLProviderResource{Name: "Example", SAMLMetadataDocument: strings.Repeat("x", 1000)}
			out, err := r.Create(t.Context(), f.configuration())
			require.Error(t, err)
			require.NotErrorIs(t, err, runtime.ErrNotFound)
			require.Nil(t, out)
			want := []string{"CreateSAMLProvider", "GetSAMLProvider", "DeleteSAMLProvider"}
			if mode == "no-arn" {
				want = []string{"CreateSAMLProvider"}
			}
			require.Equal(t, want, samlActions(f))
		})
	}
}

func TestSAMLProviderUpdateErrors(t *testing.T) {
	for _, failure := range []string{"UntagSAMLProvider", "TagSAMLProvider", "UpdateSAMLProvider"} {
		t.Run(failure, func(t *testing.T) {
			f := newFakeIAM(t)
			for _, action := range []string{"UntagSAMLProvider", "TagSAMLProvider", "UpdateSAMLProvider"} {
				f.on(action, func(_ int, _ url.Values) (int, string) {
					if action == failure {
						return samlError("AccessDenied")
					}
					return 200, samlResult(action, "")
				})
			}
			tags := map[string]string{"add": ""}
			r := SAMLProviderResource{Name: "Example", SAMLMetadataDocument: "raw desired", Tags: &tags}
			out, err := r.Update(t.Context(), f.configuration(),
				runtime.Prior[SAMLProviderResource, *SAMLProviderResourceOutput, *awsCfg]{
					Outputs: &SAMLProviderResourceOutput{Arn: samlTestARN,
						SAMLMetadataDocument: "old", Tags: map[string]string{"remove": "1"}},
				})
			require.ErrorContains(t, err, "AccessDenied")
			require.Nil(t, out)
			want := []string{"UntagSAMLProvider"}
			if failure != "UntagSAMLProvider" {
				want = append(want, "TagSAMLProvider")
			}
			if failure == "UpdateSAMLProvider" {
				want = append(want, "UpdateSAMLProvider")
			}
			require.Equal(t, want, samlActions(f))
		})
	}
}
