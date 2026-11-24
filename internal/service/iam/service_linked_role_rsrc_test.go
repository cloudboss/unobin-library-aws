package iam

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const linkedARN = "arn:aws:iam::123456789012:role/aws-service-role/" +
	"test.amazonaws.com/Assigned_suffix"

func linkedRoleXML(action, description string) string {
	return fmt.Sprintf(`<%[1]sResponse><%[1]sResult><Role>
 <Arn>%[2]s</Arn><RoleName>Assigned_suffix</RoleName><RoleId>AROA123</RoleId>
 <Path>/aws-service-role/test.amazonaws.com/</Path><CreateDate>2026-01-02T03:04:05Z</CreateDate>
 <Description>%[3]s</Description></Role></%[1]sResult></%[1]sResponse>`,
		action, linkedARN, description)
}

func linkedError(code string) (int, string) {
	return 400, fmt.Sprintf(`<ErrorResponse><Error><Code>%s</Code><Message>blocked</Message>
 </Error></ErrorResponse>`, code)
}

func linkedFakeClock() (serviceLinkedRoleClock, *[]time.Duration) {
	now := time.Unix(0, 0)
	sleeps := []time.Duration{}
	return serviceLinkedRoleClock{now: func() time.Time { return now },
		sleep: func(ctx context.Context, d time.Duration) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			sleeps = append(sleeps, d)
			now = now.Add(d)
			return nil
		}}, &sleeps
}

func linkedTags(fake *fakeIAM, tags string) {
	fake.on("ListRoleTags", func(int, url.Values) (int, string) {
		return 200, "<ListRoleTagsResponse><ListRoleTagsResult><Tags>" + tags +
			"</Tags><IsTruncated>false</IsTruncated></ListRoleTagsResult></ListRoleTagsResponse>"
	})
}

func linkedRequests(fake *fakeIAM) []url.Values {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]url.Values(nil), fake.requests...)
}

func linkedForm(action string, fields ...string) url.Values {
	form := url.Values{"Action": {action}, "Version": {"2010-05-08"}}
	for i := 0; i < len(fields); i += 2 {
		form.Set(fields[i], fields[i+1])
	}
	return form
}

func TestServiceLinkedRoleIdentity(t *testing.T) {
	for _, tc := range []struct {
		identity, name, service, suffix string
		invalid                         bool
	}{
		{linkedARN, "Assigned_suffix", "test.amazonaws.com", "suffix", false},
		{"arn:aws:iam::123:any/thing/./NoSuffix", "NoSuffix", ".", "", false},
		{"arn:aws:iam::123:any/thing/svc/Many_under_scores", "Many_under_scores", "svc", "", false},
		{"not-an-arn", "", "", "", true},
		{"arn:aws:iam::123:role/too/few", "", "", "", true},
		{"arn:aws:iam::123:role/too/many/path/parts", "", "", "", true},
	} {
		t.Run(tc.identity, func(t *testing.T) {
			got, err := (ServiceLinkedRoleResource{}).prepare(tc.identity)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.name, got.name)
			assert.Equal(t, tc.service, got.service)
			assert.Equal(t, tc.suffix, got.suffix)
		})
	}
}

func TestServiceLinkedRoleCreate(t *testing.T) {
	for _, mode := range []string{"nil", "empty", "explicit"} {
		t.Run(mode, func(t *testing.T) {
			fake := newFakeIAM(t)
			fake.on("CreateServiceLinkedRole", func(int, url.Values) (int, string) {
				return 200, linkedRoleXML("CreateServiceLinkedRole", "description")
			})
			fake.on("TagRole", func(int, url.Values) (int, string) {
				return 200, emptyIAMResultXML("TagRole")
			})
			fake.on("GetRole", func(int, url.Values) (int, string) {
				return 200, linkedRoleXML("GetRole", "description")
			})
			linkedTags(fake, `<member><Key>owner</Key><Value></Value></member>`)
			tags := map[string]string{"owner": "", "aws:reserved": "ignored"}
			resource := &ServiceLinkedRoleResource{AWSServiceName: "test.amazonaws.com", Tags: &tags}
			wantCreate := linkedForm("CreateServiceLinkedRole", "AWSServiceName", "test.amazonaws.com")
			if mode == "empty" {
				resource.CustomSuffix, resource.Description = aws.String(""), aws.String("")
			}
			if mode == "explicit" {
				resource.CustomSuffix, resource.Description = aws.String("suffix"), aws.String("description")
				wantCreate.Set("CustomSuffix", "suffix")
				wantCreate.Set("Description", "description")
			}
			out, err := resource.Create(t.Context(), fake.configuration())
			require.NoError(t, err)
			assert.Equal(t, &ServiceLinkedRoleResourceOutput{
				Arn: linkedARN, RoleName: "Assigned_suffix", RoleId: "AROA123",
				Path: "/aws-service-role/test.amazonaws.com/", CreateDate: "2026-01-02T03:04:05Z",
				Description: "description", AWSServiceName: "test.amazonaws.com", CustomSuffix: "suffix",
				Tags: map[string]string{"owner": ""},
			}, out)
			assert.Equal(t, []url.Values{
				wantCreate,
				linkedForm("TagRole", "RoleName", "Assigned_suffix", "Tags.member.1.Key", "owner",
					"Tags.member.1.Value", ""),
				linkedForm("GetRole", "RoleName", "Assigned_suffix"),
				linkedForm("ListRoleTags", "RoleName", "Assigned_suffix"),
			}, linkedRequests(fake))
		})
	}
}

func TestServiceLinkedRoleCreateRetry(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		failures   int
		want       string
	}{
		{"transient", "AccessDenied", 2, ""},
		{"exhausted", "AccessDenied", 100, "AccessDenied"},
		{"exact code", "AccessDeniedException", 100, "AccessDeniedException"},
		{"unrelated", "InvalidInput", 100, "InvalidInput"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeIAM(t)
			fake.on("CreateServiceLinkedRole", func(n int, _ url.Values) (int, string) {
				if n <= tc.failures {
					return linkedError(tc.code)
				}
				return 200, linkedRoleXML("CreateServiceLinkedRole", "")
			})
			fake.on("GetRole", func(int, url.Values) (int, string) {
				return 200, linkedRoleXML("GetRole", "")
			})
			linkedTags(fake, "")
			cfg := fake.configuration()
			client, err := newClient(t.Context(), cfg)
			require.NoError(t, err)
			clock, sleeps := linkedFakeClock()
			_, err = (&ServiceLinkedRoleResource{AWSServiceName: "test.amazonaws.com"}).
				createServiceLinkedRole(t.Context(), client, clock)
			if tc.want == "" {
				require.NoError(t, err)
				assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second}, *sleeps)
			} else {
				require.ErrorContains(t, err, tc.want)
				if tc.code != "AccessDenied" {
					assert.Empty(t, *sleeps)
				}
			}
			for _, request := range fake.sent("CreateServiceLinkedRole") {
				assert.Equal(t, linkedForm("CreateServiceLinkedRole", "AWSServiceName", "test.amazonaws.com"),
					request)
			}
		})
	}
}

func TestServiceLinkedRoleReadPropagation(t *testing.T) {
	for _, mode := range []string{"transient", "exhausted", "established", "empty", "other"} {
		t.Run(mode, func(t *testing.T) {
			fake := newFakeIAM(t)
			fake.on("GetRole", func(n int, _ url.Values) (int, string) {
				if mode == "other" {
					return linkedError("InvalidInput")
				}
				if mode == "empty" {
					return 200, emptyIAMResultXML("GetRole")
				}
				if mode != "transient" || n < 3 {
					return linkedError("NoSuchEntity")
				}
				return 200, linkedRoleXML("GetRole", "")
			})
			linkedTags(fake, "")
			client, err := newClient(t.Context(), fake.configuration())
			require.NoError(t, err)
			clock, sleeps := linkedFakeClock()
			out, err := readServiceLinkedRole(t.Context(), client, linkedARN, mode != "established", clock)
			switch mode {
			case "transient":
				require.NoError(t, err)
				assert.Equal(t, linkedARN, out.Arn)
				assert.Equal(t, []time.Duration{500 * time.Millisecond, time.Second}, *sleeps)
			case "other":
				require.ErrorContains(t, err, "InvalidInput")
				assert.Empty(t, *sleeps)
			default:
				require.ErrorIs(t, err, runtime.ErrNotFound)
			}
		})
	}
}

func TestServiceLinkedRoleUpdateObservations(t *testing.T) {
	for _, mode := range []string{"removed", "empty", "drift", "tag only", "unchanged"} {
		t.Run(mode, func(t *testing.T) {
			fake := newFakeIAM(t)
			description := "desired"
			currentDescription := "external"
			desired := map[string]string{"keep": "1", "change": "new", "add": ""}
			current := map[string]string{"keep": "1", "change": "old", "drop": "yes", "aws:owner": "system"}
			if mode == "removed" || mode == "empty" {
				description = ""
			}
			if mode == "tag only" || mode == "unchanged" {
				currentDescription = description
			}
			if mode == "unchanged" {
				current = desired
			}
			for _, action := range []string{"TagRole", "UntagRole", "UpdateRole"} {
				fake.on(action, func(_ int, form url.Values) (int, string) {
					return 200, emptyIAMResultXML(form.Get("Action"))
				})
			}
			fake.on("GetRole", func(int, url.Values) (int, string) {
				return 200, linkedRoleXML("GetRole", description)
			})
			linkedTags(fake, `<member><Key>add</Key><Value></Value></member>`+
				`<member><Key>change</Key><Value>new</Value></member>`+
				`<member><Key>keep</Key><Value>1</Value></member>`)
			r := &ServiceLinkedRoleResource{AWSServiceName: "test.amazonaws.com", Tags: &desired}
			if mode != "removed" {
				r.Description = &description
			}
			prior := runtime.Prior[ServiceLinkedRoleResource, *ServiceLinkedRoleResourceOutput, *awsCfg]{
				Inputs: *r, Outputs: &ServiceLinkedRoleResourceOutput{Arn: linkedARN},
				Observed: &ServiceLinkedRoleResourceOutput{Description: currentDescription, Tags: current},
			}
			out, err := r.Update(t.Context(), fake.configuration(), prior)
			require.NoError(t, err)
			assert.Equal(t, description, out.Description)
			assert.Equal(t, desired, out.Tags)
			requests := linkedRequests(fake)
			actions := []string{}
			for _, request := range requests {
				actions = append(actions, request.Get("Action"))
			}
			want := []string{}
			if mode != "unchanged" {
				want = append(want, "UntagRole", "TagRole")
			}
			if mode == "removed" || mode == "empty" || mode == "drift" {
				want = append(want, "UpdateRole")
			}
			want = append(want, "GetRole", "ListRoleTags")
			assert.Equal(t, want, actions)
			if mode != "unchanged" {
				assert.Equal(t, []url.Values{linkedForm("UntagRole", "RoleName", "Assigned_suffix",
					"TagKeys.member.1", "drop")}, fake.sent("UntagRole"))
				tags := fake.sent("TagRole")[0]
				got := map[string]string{}
				for i := 1; i <= 2; i++ {
					p := fmt.Sprintf("Tags.member.%d.", i)
					got[tags.Get(p+"Key")] = tags.Get(p + "Value")
				}
				assert.Equal(t, map[string]string{"change": "new", "add": ""}, got)
			}
			if mode == "removed" || mode == "empty" || mode == "drift" {
				assert.Equal(t, []url.Values{linkedForm("UpdateRole", "RoleName", "Assigned_suffix",
					"Description", description)}, fake.sent("UpdateRole"))
			}
		})
	}
}

func TestServiceLinkedRoleTagFailures(t *testing.T) {
	for _, action := range []string{"UntagRole", "TagRole"} {
		t.Run(action, func(t *testing.T) {
			fake := newFakeIAM(t)
			fake.on("UntagRole", func(int, url.Values) (int, string) {
				if action == "UntagRole" {
					return linkedError("InvalidInput")
				}
				return 200, emptyIAMResultXML("UntagRole")
			})
			fake.on("TagRole", func(int, url.Values) (int, string) { return linkedError("InvalidInput") })
			client, err := newClient(t.Context(), fake.configuration())
			require.NoError(t, err)
			err = syncServiceLinkedRoleTags(t.Context(), client, "Assigned_suffix",
				map[string]string{"drop": "x"}, map[string]string{"add": ""})
			require.ErrorContains(t, err, "InvalidInput")
			if action == "UntagRole" {
				assert.Empty(t, fake.sent("TagRole"))
			}
		})
	}
}

func TestServiceLinkedRoleCancellation(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("CreateServiceLinkedRole", func(int, url.Values) (int, string) {
		return linkedError("AccessDenied")
	})
	client, err := newClient(t.Context(), fake.configuration())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	clock, _ := linkedFakeClock()
	clock.sleep = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
	resource := &ServiceLinkedRoleResource{AWSServiceName: "."}
	_, err = resource.createServiceLinkedRole(ctx, client, clock)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []url.Values{
		linkedForm("CreateServiceLinkedRole", "AWSServiceName", "."),
	}, fake.sent("CreateServiceLinkedRole"))
}

func TestServiceLinkedRoleDelete(t *testing.T) {
	for _, mode := range []string{"success", "missing role", "no task", "missing task", "empty task",
		"failed", "unexpected", "other error", "delete error", "timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			fake := newFakeIAM(t)
			fake.on("DeleteServiceLinkedRole", func(int, url.Values) (int, string) {
				if mode == "missing role" {
					return linkedError("NoSuchEntity")
				}
				if mode == "delete error" {
					return linkedError("InvalidInput")
				}
				task := "task-123"
				if mode == "no task" {
					task = ""
				}
				return 200, "<DeleteServiceLinkedRoleResponse><DeleteServiceLinkedRoleResult>" +
					"<DeletionTaskId>" + task + "</DeletionTaskId>" +
					"</DeleteServiceLinkedRoleResult></DeleteServiceLinkedRoleResponse>"
			})
			fake.on("GetServiceLinkedRoleDeletionStatus", func(n int, _ url.Values) (int, string) {
				state := "IN_PROGRESS"
				switch mode {
				case "success":
					if n == 1 {
						state = "NOT_STARTED"
					}
					if n == 3 {
						state = "SUCCEEDED"
					}
				case "missing task":
					return linkedError("NoSuchEntity")
				case "empty task":
					return 200, emptyIAMResultXML("GetServiceLinkedRoleDeletionStatus")
				case "failed":
					state = "FAILED"
				case "unexpected":
					state = "UNKNOWN"
				case "other error":
					return linkedError("InvalidInput")
				}
				return 200, "<GetServiceLinkedRoleDeletionStatusResponse>" +
					"<GetServiceLinkedRoleDeletionStatusResult><Status>" + state + "</Status>" +
					"<Reason><Reason>in use</Reason><RoleUsageList>" +
					"<member><Region>us-east-1</Region><Resources><member>resource-one</member>" +
					"<member>resource-two</member></Resources></member>" +
					"<member><Region>eu-west-1</Region><Resources><member>resource-three</member>" +
					"</Resources></member></RoleUsageList></Reason>" +
					"</GetServiceLinkedRoleDeletionStatusResult></GetServiceLinkedRoleDeletionStatusResponse>"
			})
			client, err := newClient(t.Context(), fake.configuration())
			require.NoError(t, err)
			clock, sleeps := linkedFakeClock()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancel" {
				clock.sleep = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
			}
			err = deleteServiceLinkedRole(ctx, client, linkedARN, clock)
			switch mode {
			case "success", "missing role", "no task":
				require.NoError(t, err)
			case "missing task":
				require.ErrorContains(t, err, "missing after 20 retries")
			case "empty task":
				require.ErrorContains(t, err, "deletion")
				assert.Equal(t, []time.Duration{10 * time.Second}, *sleeps)
			case "failed", "unexpected":
				for _, detail := range []string{"in use", "us-east-1", "eu-west-1", "resource-one",
					"resource-two", "resource-three"} {
					require.ErrorContains(t, err, detail)
				}
			case "other error", "delete error":
				require.ErrorContains(t, err, "InvalidInput")
			case "timeout":
				require.ErrorContains(t, err, "timed out")
			case "cancel":
				require.ErrorIs(t, err, context.Canceled)
			}
			assert.Equal(t, []url.Values{
				linkedForm("DeleteServiceLinkedRole", "RoleName", "Assigned_suffix")},
				fake.sent("DeleteServiceLinkedRole"))
			for _, request := range fake.sent("GetServiceLinkedRoleDeletionStatus") {
				assert.Equal(t, linkedForm("GetServiceLinkedRoleDeletionStatus",
					"DeletionTaskId", "task-123"), request)
			}
			if mode == "success" {
				assert.Equal(t, []time.Duration{
					10 * time.Second, 200 * time.Millisecond, 400 * time.Millisecond,
				}, *sleeps)
			}
		})
	}
}

func TestServiceLinkedRoleReadTagPages(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("GetRole", func(int, url.Values) (int, string) {
		return 200, linkedRoleXML("GetRole", "")
	})
	fake.on("ListRoleTags", func(n int, _ url.Values) (int, string) {
		if n == 1 {
			return 200, `<ListRoleTagsResponse><ListRoleTagsResult><Tags>
   <member><Key>keep</Key><Value>1</Value></member>
   <member><Key>aws:managed</Key><Value>system</Value></member></Tags>
   <IsTruncated>true</IsTruncated><Marker>next-page</Marker>
   </ListRoleTagsResult></ListRoleTagsResponse>`
		}
		return 200, `<ListRoleTagsResponse><ListRoleTagsResult><Tags>
   <member><Key>empty</Key><Value></Value></member></Tags><IsTruncated>false</IsTruncated>
   </ListRoleTagsResult></ListRoleTagsResponse>`
	})
	client, err := newClient(t.Context(), fake.configuration())
	require.NoError(t, err)
	clock, _ := linkedFakeClock()
	output, err := readServiceLinkedRole(t.Context(), client, linkedARN, false, clock)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"keep": "1", "empty": ""}, output.Tags)
	assert.Equal(t, []url.Values{
		linkedForm("GetRole", "RoleName", "Assigned_suffix"),
		linkedForm("ListRoleTags", "RoleName", "Assigned_suffix"),
		linkedForm("ListRoleTags", "RoleName", "Assigned_suffix", "Marker", "next-page"),
	}, linkedRequests(fake))
}

func TestServiceLinkedRoleCreateFailureStops(t *testing.T) {
	for _, mode := range []string{"no role", "no ARN", "bad ARN", "wrong parts", "empty name",
		"tag failure"} {
		t.Run(mode, func(t *testing.T) {
			fake := newFakeIAM(t)
			fake.on("CreateServiceLinkedRole", func(int, url.Values) (int, string) {
				switch mode {
				case "no role":
					return 200, emptyIAMResultXML("CreateServiceLinkedRole")
				case "no ARN":
					return 200, `<CreateServiceLinkedRoleResponse>
     <CreateServiceLinkedRoleResult><Role><RoleName>assigned</RoleName></Role>
     </CreateServiceLinkedRoleResult></CreateServiceLinkedRoleResponse>`
				case "bad ARN":
					return 200, `<CreateServiceLinkedRoleResponse>
     <CreateServiceLinkedRoleResult><Role><Arn>invalid</Arn></Role>
     </CreateServiceLinkedRoleResult></CreateServiceLinkedRoleResponse>`
				case "wrong parts", "empty name":
					identity := "arn:aws:iam::123:role/too/few"
					if mode == "empty name" {
						identity = "arn:aws:iam::123:role/aws-service-role/test.amazonaws.com/"
					}
					return 200, strings.ReplaceAll(
						linkedRoleXML("CreateServiceLinkedRole", ""), linkedARN, identity)
				default:
					return 200, linkedRoleXML("CreateServiceLinkedRole", "")
				}
			})
			fake.on("TagRole", func(int, url.Values) (int, string) { return linkedError("InvalidInput") })
			fake.on("DeleteServiceLinkedRole", func(int, url.Values) (int, string) {
				return 200, emptyIAMResultXML("DeleteServiceLinkedRole")
			})
			tags := map[string]string{"owner": ""}
			_, err := (&ServiceLinkedRoleResource{AWSServiceName: ".", Tags: &tags}).
				Create(t.Context(), fake.configuration())
			require.Error(t, err)
			if mode == "tag failure" {
				require.ErrorContains(t, err, "tag service-linked role")
				assert.Equal(t, []url.Values{
					linkedForm("DeleteServiceLinkedRole", "RoleName", "Assigned_suffix"),
				}, fake.sent("DeleteServiceLinkedRole"))
			} else {
				assert.Empty(t, fake.sent("TagRole"))
				assert.Empty(t, fake.sent("DeleteServiceLinkedRole"))
			}
			assert.Empty(t, fake.sent("GetRole"))
			assert.Empty(t, fake.sent("ListRoleTags"))
		})
	}
}

func TestServiceLinkedRoleInvalidIdentityHasNoRequests(t *testing.T) {
	fake := newFakeIAM(t)
	cfg := fake.configuration()
	r := &ServiceLinkedRoleResource{AWSServiceName: "invalid historical input"}
	prior := runtime.Prior[ServiceLinkedRoleResource, *ServiceLinkedRoleResourceOutput, *awsCfg]{
		Outputs: &ServiceLinkedRoleResourceOutput{Arn: "invalid"},
	}
	_, err := r.Read(t.Context(), cfg, prior)
	require.ErrorContains(t, err, "ARN")
	_, err = r.Update(t.Context(), cfg, prior)
	require.ErrorContains(t, err, "ARN")
	err = r.Delete(t.Context(), cfg, prior)
	require.ErrorContains(t, err, "ARN")
	require.Empty(t, linkedRequests(fake))
}

func TestServiceLinkedRoleReadCancellation(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("GetRole", func(int, url.Values) (int, string) { return linkedError("NoSuchEntity") })
	client, err := newClient(t.Context(), fake.configuration())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	clock, _ := linkedFakeClock()
	clock.sleep = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
	_, err = readServiceLinkedRole(ctx, client, linkedARN, true, clock)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []url.Values{linkedForm("GetRole", "RoleName", "Assigned_suffix")},
		linkedRequests(fake))
}

func TestServiceLinkedRoleReadTagError(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("GetRole", func(int, url.Values) (int, string) {
		return 200, linkedRoleXML("GetRole", "")
	})
	fake.on("ListRoleTags", func(int, url.Values) (int, string) { return linkedError("InvalidInput") })
	client, err := newClient(t.Context(), fake.configuration())
	require.NoError(t, err)
	clock, _ := linkedFakeClock()
	_, err = readServiceLinkedRole(t.Context(), client, linkedARN, false, clock)
	require.ErrorContains(t, err, "list service-linked role tags "+linkedARN)
	require.ErrorContains(t, err, "InvalidInput")
}

func TestServiceLinkedRoleUpdateFailureStops(t *testing.T) {
	for _, action := range []string{"UntagRole", "TagRole", "UpdateRole"} {
		t.Run(action, func(t *testing.T) {
			fake := newFakeIAM(t)
			for _, operation := range []string{"UntagRole", "TagRole", "UpdateRole"} {
				fake.on(operation, func(_ int, form url.Values) (int, string) {
					if form.Get("Action") == action {
						return linkedError("InvalidInput")
					}
					return 200, emptyIAMResultXML(form.Get("Action"))
				})
			}
			tags := map[string]string{"new": ""}
			resource := &ServiceLinkedRoleResource{AWSServiceName: "test.amazonaws.com", Tags: &tags}
			_, err := resource.Update(t.Context(), fake.configuration(), runtime.Prior[
				ServiceLinkedRoleResource, *ServiceLinkedRoleResourceOutput, *awsCfg,
			]{
				Inputs: *resource, Outputs: &ServiceLinkedRoleResourceOutput{Arn: linkedARN},
				Observed: &ServiceLinkedRoleResourceOutput{
					Description: "remove", Tags: map[string]string{"old": "x"},
				},
			})
			require.ErrorContains(t, err, "InvalidInput")
			want := []string{"UntagRole"}
			if action != "UntagRole" {
				want = append(want, "TagRole")
			}
			if action == "UpdateRole" {
				want = append(want, "UpdateRole")
			}
			got := []string{}
			for _, request := range linkedRequests(fake) {
				got = append(got, request.Get("Action"))
			}
			require.Equal(t, want, got)
		})
	}
}

func TestServiceLinkedRoleDeletionMissingBudgetResets(t *testing.T) {
	fake := newFakeIAM(t)
	fake.on("DeleteServiceLinkedRole", func(int, url.Values) (int, string) {
		return 200, `<DeleteServiceLinkedRoleResponse><DeleteServiceLinkedRoleResult>
   <DeletionTaskId>task</DeletionTaskId>
   </DeleteServiceLinkedRoleResult></DeleteServiceLinkedRoleResponse>`
	})
	fake.on("GetServiceLinkedRoleDeletionStatus", func(n int, _ url.Values) (int, string) {
		if n != 21 && n != 23 {
			return linkedError("NoSuchEntity")
		}
		state := "IN_PROGRESS"
		if n == 23 {
			state = "SUCCEEDED"
		}
		return 200, "<GetServiceLinkedRoleDeletionStatusResponse>" +
			"<GetServiceLinkedRoleDeletionStatusResult><Status>" + state + "</Status>" +
			"</GetServiceLinkedRoleDeletionStatusResult></GetServiceLinkedRoleDeletionStatusResponse>"
	})
	client, err := newClient(t.Context(), fake.configuration())
	require.NoError(t, err)
	clock, sleeps := linkedFakeClock()
	require.NoError(t, deleteServiceLinkedRole(t.Context(), client, linkedARN, clock))
	want := []url.Values{}
	intervals := []time.Duration{10 * time.Second}
	delay := 200 * time.Millisecond
	for i := range 23 {
		want = append(want, linkedForm("GetServiceLinkedRoleDeletionStatus", "DeletionTaskId", "task"))
		if i != 0 {
			intervals = append(intervals, delay)
			delay = min(2*delay, 10*time.Second)
		}
	}
	require.Equal(t, want, fake.sent("GetServiceLinkedRoleDeletionStatus"))
	require.Equal(t, intervals, *sleeps)
}

func TestServiceLinkedRoleBackoff(t *testing.T) {
	clock, sleeps := linkedFakeClock()
	backoff := serviceLinkedRoleBackoff{
		clock: clock, deadline: clock.now().Add(2 * time.Minute), delay: 500 * time.Millisecond,
	}
	for range 17 {
		again, err := backoff.wait(t.Context())
		require.NoError(t, err)
		require.True(t, again)
	}
	again, err := backoff.wait(t.Context())
	require.NoError(t, err)
	require.False(t, again)
	want := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second,
		4 * time.Second, 8 * time.Second}
	for range 12 {
		want = append(want, 10*time.Second)
	}
	assert.Equal(t, want, *sleeps)
	assert.Equal(t, time.Unix(0, 0).Add(135500*time.Millisecond), clock.now())
}

func TestServiceLinkedRolePropagationBoundary(t *testing.T) {
	for _, operation := range []string{"create", "read"} {
		for _, success := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/success=%t", operation, success), func(t *testing.T) {
				fake := newFakeIAM(t)
				clock, sleeps := linkedFakeClock()
				start := clock.now()
				attempts := []time.Duration{}
				action, code := "CreateServiceLinkedRole", "AccessDenied"
				if operation == "read" {
					action, code = "GetRole", "NoSuchEntity"
				} else {
					fake.on("GetRole", func(int, url.Values) (int, string) {
						return 200, linkedRoleXML("GetRole", "")
					})
				}
				fake.on(action, func(n int, _ url.Values) (int, string) {
					attempts = append(attempts, clock.now().Sub(start))
					if success && n == 18 {
						return 200, linkedRoleXML(action, "")
					}
					return linkedError(code)
				})
				linkedTags(fake, "")
				client, err := newClient(t.Context(), fake.configuration())
				require.NoError(t, err)
				if operation == "create" {
					_, err = (&ServiceLinkedRoleResource{AWSServiceName: "."}).
						createServiceLinkedRole(t.Context(), client, clock)
				} else {
					_, err = readServiceLinkedRole(t.Context(), client, linkedARN, true, clock)
				}
				if success {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, code)
				}
				want := []time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond,
					3500 * time.Millisecond, 7500 * time.Millisecond, 15500 * time.Millisecond}
				for i := range 12 {
					want = append(want, time.Duration(25500+i*10000)*time.Millisecond)
				}
				assert.Equal(t, want, attempts)
				assert.Equal(t, want[len(want)-1], clock.now().Sub(start))
				assert.Equal(t, []time.Duration{
					500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second,
					8 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second,
					10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second,
					10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second,
					10 * time.Second,
				}, *sleeps)
			})
		}
	}
}

type linkedDeletionStub struct {
	t       *testing.T
	refresh func(context.Context) (*iam.GetServiceLinkedRoleDeletionStatusOutput, error)
}

func (s linkedDeletionStub) DeleteServiceLinkedRole(
	_ context.Context, in *iam.DeleteServiceLinkedRoleInput, _ ...func(*iam.Options),
) (*iam.DeleteServiceLinkedRoleOutput, error) {
	require.Equal(s.t, &iam.DeleteServiceLinkedRoleInput{RoleName: aws.String("Assigned_suffix")}, in)
	return &iam.DeleteServiceLinkedRoleOutput{DeletionTaskId: aws.String("task")}, nil
}

func (s linkedDeletionStub) GetServiceLinkedRoleDeletionStatus(
	ctx context.Context, in *iam.GetServiceLinkedRoleDeletionStatusInput, _ ...func(*iam.Options),
) (*iam.GetServiceLinkedRoleDeletionStatusOutput, error) {
	require.Equal(s.t, &iam.GetServiceLinkedRoleDeletionStatusInput{
		DeletionTaskId: aws.String("task"),
	}, in)
	return s.refresh(ctx)
}

func TestServiceLinkedRoleDeletionZeroClassification(t *testing.T) {
	for _, mode := range []string{"nil", "zero", "missing", "reason", "metadata", "empty reason"} {
		t.Run(mode, func(t *testing.T) {
			clock, sleeps := linkedFakeClock()
			calls := 0
			client := linkedDeletionStub{t: t, refresh: func(context.Context) (
				*iam.GetServiceLinkedRoleDeletionStatusOutput, error,
			) {
				calls++
				out := &iam.GetServiceLinkedRoleDeletionStatusOutput{}
				switch mode {
				case "nil":
					return nil, nil
				case "missing":
					return nil, &iamtypes.NoSuchEntityException{Message: aws.String("missing")}
				case "reason":
					out.Reason = &iamtypes.DeletionTaskFailureReasonType{
						Reason: aws.String("in use"),
						RoleUsageList: []iamtypes.RoleUsageType{
							{Region: aws.String("east"), Resources: []string{"one", "two"}},
							{Region: aws.String("west"), Resources: []string{"three"}},
						},
					}
				case "metadata":
					out.ResultMetadata.Set("request-id", "id")
				case "empty reason":
					out.Reason = &iamtypes.DeletionTaskFailureReasonType{}
				}
				return out, nil
			}}
			err := deleteServiceLinkedRole(t.Context(), client, linkedARN, clock)
			if mode == "nil" || mode == "zero" || mode == "missing" {
				require.ErrorContains(t, err, "missing after 20 retries")
				assert.Equal(t, 21, calls)
				want := []time.Duration{10 * time.Second, 200 * time.Millisecond,
					400 * time.Millisecond, 800 * time.Millisecond, 1600 * time.Millisecond,
					3200 * time.Millisecond, 6400 * time.Millisecond}
				for range 14 {
					want = append(want, 10*time.Second)
				}
				assert.Equal(t, want, *sleeps)
			} else {
				require.ErrorContains(t, err, "deletion")
				assert.Equal(t, 1, calls)
				assert.Equal(t, []time.Duration{10 * time.Second}, *sleeps)
				if mode == "reason" {
					for _, detail := range []string{"in use", "east", "west", "one", "two", "three"} {
						assert.ErrorContains(t, err, detail)
					}
				}
			}
		})
	}
}

func TestServiceLinkedRoleDeletionRefreshBudget(t *testing.T) {
	for _, mode := range []string{"remaining", "parent deadline", "active expiry", "late success",
		"expired refresh", "parent cancel", "parent expiry", "other error", "late active success"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			start := now
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "parent deadline" || mode == "parent expiry" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 20*time.Millisecond)
				defer stop()
			}
			clock := serviceLinkedRoleClock{now: func() time.Time { return now },
				sleep: func(_ context.Context, d time.Duration) error {
					now = now.Add(d)
					if mode == "active expiry" || mode == "late active success" {
						now = start.Add(5*time.Minute - 10*time.Millisecond)
					}
					return nil
				}}
			calls := 0
			client := linkedDeletionStub{t: t, refresh: func(child context.Context) (
				*iam.GetServiceLinkedRoleDeletionStatusOutput, error,
			) {
				calls++
				deadline, ok := child.Deadline()
				require.True(t, ok, "status refresh needs a deadline")
				switch mode {
				case "remaining":
					want := 5*time.Minute - now.Sub(start)
					assert.InDelta(t, want.Seconds(), time.Until(deadline).Seconds(), 0.1)
					if calls < 3 {
						return &iam.GetServiceLinkedRoleDeletionStatusOutput{Status: "IN_PROGRESS"}, nil
					}
				case "parent deadline":
					parentDeadline, _ := ctx.Deadline()
					assert.Equal(t, parentDeadline, deadline)
				case "active expiry":
					<-child.Done()
					now = start.Add(5 * time.Minute)
					return nil, child.Err()
				case "late success":
					if calls == 1 {
						now = start.Add(6 * time.Minute)
						return &iam.GetServiceLinkedRoleDeletionStatusOutput{Status: "IN_PROGRESS"}, nil
					}
					require.ErrorIs(t, child.Err(), context.DeadlineExceeded)
				case "late active success":
					<-child.Done()
					now = start.Add(6 * time.Minute)
				case "expired refresh":
					if calls == 1 {
						now = start.Add(5 * time.Minute)
						return &iam.GetServiceLinkedRoleDeletionStatusOutput{Status: "IN_PROGRESS"}, nil
					}
					require.ErrorIs(t, child.Err(), context.DeadlineExceeded)
					return nil, child.Err()
				case "parent cancel":
					cancel()
					return nil, child.Err()
				case "parent expiry":
					<-child.Done()
					return nil, child.Err()
				case "other error":
					return nil, errors.New("refresh failed")
				}
				return &iam.GetServiceLinkedRoleDeletionStatusOutput{Status: "SUCCEEDED"}, nil
			}}
			err := deleteServiceLinkedRole(ctx, client, linkedARN, clock)
			switch mode {
			case "active expiry", "expired refresh":
				require.ErrorContains(t, err, "timed out")
			case "parent cancel":
				require.ErrorIs(t, err, context.Canceled)
			case "parent expiry":
				require.ErrorIs(t, err, context.DeadlineExceeded)
				assert.NotContains(t, err.Error(), "timed out")
			case "other error":
				require.ErrorContains(t, err, "refresh failed")
			default:
				require.NoError(t, err)
			}
			switch mode {
			case "remaining":
				assert.Equal(t, 3, calls)
			case "expired refresh", "late success":
				assert.Equal(t, 2, calls)
			default:
				assert.Equal(t, 1, calls)
			}
		})
	}
}

type linkedHTTPClient func(*http.Request) (*http.Response, error)

type linkedContextKey struct{}

func (f linkedHTTPClient) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestServiceLinkedRoleIndependentPropagationBudgets(t *testing.T) {
	fake := newFakeIAM(t)
	clock, _ := linkedFakeClock()
	start := clock.now()
	attempts := map[string][]time.Duration{}
	for _, action := range []string{"CreateServiceLinkedRole", "GetRole"} {
		fake.on(action, func(n int, _ url.Values) (int, string) {
			attempts[action] = append(attempts[action], clock.now().Sub(start))
			if n < 18 {
				code := "AccessDenied"
				if action == "GetRole" {
					code = "NoSuchEntity"
				}
				return linkedError(code)
			}
			return 200, linkedRoleXML(action, "")
		})
	}
	linkedTags(fake, "")
	client, err := newClient(t.Context(), fake.configuration())
	require.NoError(t, err)
	options := client.Options()
	transport := options.HTTPClient
	ctx := context.WithValue(t.Context(), linkedContextKey{}, "parent")
	options.HTTPClient = linkedHTTPClient(func(request *http.Request) (*http.Response, error) {
		_, hasDeadline := request.Context().Deadline()
		assert.False(t, hasDeadline, "propagation must not add a request deadline")
		assert.Equal(t, "parent", request.Context().Value(linkedContextKey{}))
		return transport.Do(request)
	})
	_, err = (&ServiceLinkedRoleResource{AWSServiceName: "."}).
		createServiceLinkedRole(ctx, iam.New(options), clock)
	require.NoError(t, err)
	want := []time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond,
		3500 * time.Millisecond, 7500 * time.Millisecond, 15500 * time.Millisecond}
	for i := range 12 {
		want = append(want, time.Duration(25500+i*10000)*time.Millisecond)
	}
	assert.Equal(t, want, attempts["CreateServiceLinkedRole"])
	readWant := make([]time.Duration, len(want))
	for i, elapsed := range want {
		readWant[i] = elapsed + 135500*time.Millisecond
	}
	assert.Equal(t, readWant, attempts["GetRole"])
}

func TestServiceLinkedRoleDeletionSoftBudget(t *testing.T) {
	clock, sleeps := linkedFakeClock()
	start := clock.now()
	attempts := []time.Duration{}
	client := linkedDeletionStub{t: t, refresh: func(ctx context.Context) (
		*iam.GetServiceLinkedRoleDeletionStatusOutput, error,
	) {
		elapsed := clock.now().Sub(start)
		attempts = append(attempts, elapsed)
		if elapsed >= 5*time.Minute {
			require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
		}
		return &iam.GetServiceLinkedRoleDeletionStatusOutput{Status: "IN_PROGRESS"}, nil
	}}
	err := deleteServiceLinkedRole(t.Context(), client, linkedARN, clock)
	require.ErrorContains(t, err, "timed out")
	want := []time.Duration{10 * time.Second, 10200 * time.Millisecond,
		10600 * time.Millisecond, 11400 * time.Millisecond, 13 * time.Second,
		16200 * time.Millisecond, 22600 * time.Millisecond}
	for i := range 29 {
		want = append(want, time.Duration(32600+i*10000)*time.Millisecond)
	}
	assert.Equal(t, want, attempts)
	intervals := make([]time.Duration, len(want))
	previous := time.Duration(0)
	for i, elapsed := range want {
		intervals[i] = elapsed - previous
		previous = elapsed
	}
	assert.Equal(t, intervals, *sleeps)
}

func TestServiceLinkedRoleAcceptedCreateCleanup(t *testing.T) {
	for _, stage := range []string{"TagRole", "GetRole", "ListRoleTags", "propagation"} {
		for _, canceled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/canceled=%t", stage, canceled), func(t *testing.T) {
				fake := newFakeIAM(t)
				client, err := newClient(t.Context(), fake.configuration())
				require.NoError(t, err)
				ctx, cancel := context.WithTimeout(
					context.WithValue(t.Context(), linkedContextKey{}, "retained"), time.Hour)
				defer cancel()
				primary := errors.New("initialization failed")
				if canceled {
					primary = context.Canceled
				}
				requests := []url.Values{}
				polls := 0
				started := time.Now()
				options := client.Options()
				options.RetryMaxAttempts = 1
				options.HTTPClient = linkedHTTPClient(func(req *http.Request) (*http.Response, error) {
					require.NoError(t, req.ParseForm())
					requests = append(requests, req.PostForm)
					action := req.PostForm.Get("Action")
					if action == stage || stage == "propagation" && action == "GetRole" {
						if canceled {
							cancel()
						}
						if stage != "propagation" || canceled {
							return nil, primary
						}
						status, body := linkedError("NoSuchEntity")
						return linkedHTTPResponse(status, body), nil
					}
					body := emptyIAMResultXML(action)
					switch action {
					case "CreateServiceLinkedRole", "GetRole":
						body = linkedRoleXML(action, "")
					case "DeleteServiceLinkedRole", "GetServiceLinkedRoleDeletionStatus":
						assert.NoError(t, req.Context().Err())
						assert.Equal(t, "retained", req.Context().Value(linkedContextKey{}))
						deadline, ok := req.Context().Deadline()
						require.True(t, ok)
						assert.WithinDuration(t, started.Add(2*time.Minute), deadline, time.Second)
						if action == "DeleteServiceLinkedRole" {
							body = linkedTaskXML("task-cleanup")
						} else {
							polls++
							state := "IN_PROGRESS"
							if polls == 2 {
								state = "SUCCEEDED"
							}
							body = linkedStatusXML(state)
						}
					}
					return linkedHTTPResponse(200, body), nil
				})
				clock, sleeps := linkedFakeClock()
				tags := map[string]string{"owner": "test"}
				out, err := (&ServiceLinkedRoleResource{AWSServiceName: ".", Tags: &tags}).
					createServiceLinkedRole(ctx, iam.New(options), clock)
				assert.Nil(t, out)
				if stage == "propagation" && !canceled {
					assert.ErrorIs(t, err, runtime.ErrNotFound)
				} else {
					assert.ErrorIs(t, err, primary)
				}
				var operation *smithy.OperationError
				assert.ErrorAs(t, err, &operation)
				require.GreaterOrEqual(t, len(requests), 4)
				assert.Equal(t, []url.Values{
					linkedForm("DeleteServiceLinkedRole", "RoleName", "Assigned_suffix"),
					linkedForm("GetServiceLinkedRoleDeletionStatus", "DeletionTaskId", "task-cleanup"),
					linkedForm("GetServiceLinkedRoleDeletionStatus", "DeletionTaskId", "task-cleanup"),
				}, requests[len(requests)-3:])
				require.GreaterOrEqual(t, len(*sleeps), 2)
				assert.Equal(t, []time.Duration{10 * time.Second, 200 * time.Millisecond},
					(*sleeps)[len(*sleeps)-2:])
			})
		}
	}
}

func linkedHTTPResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{},
		Body: io.NopCloser(strings.NewReader(body))}
}

func linkedTaskXML(task string) string {
	return "<DeleteServiceLinkedRoleResponse><DeleteServiceLinkedRoleResult><DeletionTaskId>" +
		task + "</DeletionTaskId></DeleteServiceLinkedRoleResult></DeleteServiceLinkedRoleResponse>"
}

func linkedStatusXML(status string) string {
	return "<GetServiceLinkedRoleDeletionStatusResponse><GetServiceLinkedRoleDeletionStatusResult>" +
		"<Status>" + status + "</Status>" +
		"</GetServiceLinkedRoleDeletionStatusResult></GetServiceLinkedRoleDeletionStatusResponse>"
}

type linkedCleanupStub struct {
	linkedDeletionStub
	delete func(context.Context) (*iam.DeleteServiceLinkedRoleOutput, error)
}

func (s linkedCleanupStub) DeleteServiceLinkedRole(
	ctx context.Context, in *iam.DeleteServiceLinkedRoleInput, _ ...func(*iam.Options),
) (*iam.DeleteServiceLinkedRoleOutput, error) {
	require.Equal(s.t, &iam.DeleteServiceLinkedRoleInput{RoleName: aws.String("Assigned_suffix")}, in)
	return s.delete(ctx)
}

func TestServiceLinkedRoleCleanupOutcomes(t *testing.T) {
	for _, mode := range []string{"missing", "no task", "delete error", "status error", "failed",
		"sleep timeout", "request timeout"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				primary := errors.New("initialization failed")
				secondary := &iamtypes.InvalidInputException{Message: aws.String("cleanup denied")}
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				started := time.Now()
				calls := []string{}
				check := func(ctx context.Context) {
					assert.NoError(t, ctx.Err())
					deadline, ok := ctx.Deadline()
					require.True(t, ok)
					assert.Equal(t, started.Add(2*time.Minute), deadline)
				}
				client := linkedCleanupStub{linkedDeletionStub: linkedDeletionStub{t: t}}
				client.delete = func(ctx context.Context) (*iam.DeleteServiceLinkedRoleOutput, error) {
					calls = append(calls, "delete")
					check(ctx)
					switch mode {
					case "missing":
						return nil, &iamtypes.NoSuchEntityException{}
					case "no task":
						return &iam.DeleteServiceLinkedRoleOutput{}, nil
					case "delete error":
						return nil, secondary
					}
					return &iam.DeleteServiceLinkedRoleOutput{DeletionTaskId: aws.String("task")}, nil
				}
				client.refresh = func(ctx context.Context) (*iam.GetServiceLinkedRoleDeletionStatusOutput,
					error) {
					calls = append(calls, "status")
					check(ctx)
					switch mode {
					case "status error":
						return nil, secondary
					case "request timeout":
						<-ctx.Done()
						return nil, ctx.Err()
					default:
						return &iam.GetServiceLinkedRoleDeletionStatusOutput{
							Status: "FAILED", Reason: &iamtypes.DeletionTaskFailureReasonType{
								Reason: aws.String("in use"), RoleUsageList: []iamtypes.RoleUsageType{{
									Region: aws.String("region"), Resources: []string{"resource-one", "resource-two"},
								}},
							},
						}, nil
					}
				}
				clock := realServiceLinkedRoleClock()
				if mode == "sleep timeout" {
					clock.sleep = func(ctx context.Context, _ time.Duration) error {
						check(ctx)
						<-ctx.Done()
						return ctx.Err()
					}
				}
				err := cleanupFailedServiceLinkedRoleCreate(ctx, client, linkedARN, clock, primary)
				require.ErrorIs(t, err, primary)
				want := []string{"delete"}
				switch mode {
				case "missing", "no task":
					assert.Same(t, primary, err)
				case "delete error", "status error":
					assert.ErrorIs(t, err, secondary)
					var invalid *iamtypes.InvalidInputException
					assert.ErrorAs(t, err, &invalid)
				case "sleep timeout", "request timeout":
					assert.ErrorIs(t, err, context.DeadlineExceeded)
					assert.Equal(t, 2*time.Minute, time.Since(started))
				case "failed":
					for _, detail := range []string{"FAILED", "in use", "region", "resource-one", "resource-two"} {
						assert.ErrorContains(t, err, detail)
					}
				}
				if mode == "status error" || mode == "failed" || mode == "request timeout" {
					want = append(want, "status")
				}
				if mode != "missing" && mode != "no task" {
					assert.ErrorContains(t, err, "compensating delete service-linked role "+linkedARN)
				}
				assert.Equal(t, want, calls)
			})
		})
	}
}
