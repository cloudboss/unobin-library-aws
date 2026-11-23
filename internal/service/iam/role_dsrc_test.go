package iam

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/require"
)

func TestRoleDataSourceRead(t *testing.T) {
	const encodedPolicy = "%7B%20%22z%22%3A%22%2B+%252B%22%2C%22a%22%3A1%20%7D"
	full := `<Role>
		<RoleName>Canonical</RoleName><RoleId>AROAEXAMPLE</RoleId>
		<Arn>arn:aws:iam::123456789012:role/team/Canonical</Arn><Path>/team/</Path>
		<Description>deployment role</Description><MaxSessionDuration>7200</MaxSessionDuration>
		<CreateDate>2025-04-02T03:04:05.123Z</CreateDate>
		<AssumeRolePolicyDocument>` + encodedPolicy + `</AssumeRolePolicyDocument>
		<PermissionsBoundary>
			<PermissionsBoundaryArn>arn:aws:iam::123456789012:policy/team</PermissionsBoundaryArn>
		</PermissionsBoundary>
		<RoleLastUsed>
			<Region>us-west-2</Region><LastUsedDate>2025-05-02T00:10:00Z</LastUsedDate>
		</RoleLastUsed>
		<Tags>
			<member><Key>aws:system</Key><Value>hidden</Value></member>
			<member><Key>AWS:system</Key><Value>visible</Value></member>
			<member><Key>env</Key><Value>dev</Value></member>
			<member><Key>empty</Key></member>
		</Tags>
	</Role>`
	const minimal = `<Role><CreateDate>2025-04-02T03:04:05Z</CreateDate></Role>`
	for _, tc := range []struct {
		name   string
		lookup string
		role   string
		want   *RoleDataSourceOutput
	}{
		{
			name:   "complete",
			lookup: "MiXeD_+=,.@-123",
			role:   full,
			want: &RoleDataSourceOutput{
				RoleName:                 "Canonical",
				RoleID:                   "AROAEXAMPLE",
				Arn:                      "arn:aws:iam::123456789012:role/team/Canonical",
				Path:                     "/team/",
				Description:              "deployment role",
				MaxSessionDuration:       7200,
				CreateDate:               "2025-04-02T03:04:05Z",
				AssumeRolePolicyDocument: `{ "z":"+ %2B","a":1 }`,
				PermissionsBoundary:      aws.String("arn:aws:iam::123456789012:policy/team"),
				RoleLastUsed: &RoleLastUsed{
					Region:       "us-west-2",
					LastUsedDate: aws.String("2025-05-02T00:10:00Z"),
				},
				Tags: map[string]string{"AWS:system": "visible", "env": "dev", "empty": ""},
			},
		},
		{
			name: "absent optional values and explicit empty name",
			role: minimal,
			want: &RoleDataSourceOutput{
				CreateDate: "2025-04-02T03:04:05Z",
				Tags:       map[string]string{},
			},
		},
		{
			name:   "present last-used object without region or date",
			lookup: "lookup",
			role: `<Role><CreateDate>2025-04-02T03:04:05Z</CreateDate>
				<RoleLastUsed></RoleLastUsed>
				<Tags><member><Key>aws:system</Key><Value>hidden</Value></member></Tags>
			</Role>`,
			want: &RoleDataSourceOutput{
				CreateDate:   "2025-04-02T03:04:05Z",
				RoleLastUsed: &RoleLastUsed{},
				Tags:         map[string]string{},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeIAM(t)
			f.on("GetRole", func(_ int, _ url.Values) (int, string) {
				return http.StatusOK, getRoleResponse(tc.role)
			})
			got, err := (&RoleDataSource{RoleName: tc.lookup}).Read(t.Context(), f.configuration())
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Equal(t, []url.Values{{
				"Action": {"GetRole"}, "RoleName": {tc.lookup}, "Version": {"2010-05-08"},
			}}, f.requests)
		})
	}
}

func TestRoleDataSourceLookupErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		message string
		code    string
	}{
		{
			name: "not found", status: http.StatusNotFound, code: "NoSuchEntity",
			body:    `<ErrorResponse><Error><Code>NoSuchEntity</Code></Error></ErrorResponse>`,
			message: "get role",
		},
		{
			name: "access denied", status: http.StatusForbidden, code: "AccessDenied",
			body:    `<ErrorResponse><Error><Code>AccessDenied</Code></Error></ErrorResponse>`,
			message: "get role",
		},
		{
			name: "absent role", status: http.StatusOK,
			body: getRoleResponse(""), message: "no role returned",
		},
		{
			name: "missing creation date", status: http.StatusOK,
			body: getRoleResponse("<Role></Role>"), message: "missing creation date",
		},
		{
			name: "malformed trust policy", status: http.StatusOK,
			body: getRoleResponse(`<Role><CreateDate>2025-04-02T03:04:05Z</CreateDate>
				<AssumeRolePolicyDocument>%X1</AssumeRolePolicyDocument></Role>`),
			message: "decode assume-role-policy-document",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeIAM(t)
			f.on("GetRole", func(_ int, _ url.Values) (int, string) {
				return tc.status, tc.body
			})
			got, err := (&RoleDataSource{RoleName: "lookup"}).Read(t.Context(), f.configuration())
			require.ErrorContains(t, err, tc.message)
			require.ErrorContains(t, err, `"lookup"`)
			require.Nil(t, got)
			var notFound *iamtypes.NoSuchEntityException
			require.Equal(t, tc.code == "NoSuchEntity", errors.As(err, &notFound))
			if tc.code != "" {
				var apiError smithy.APIError
				require.ErrorAs(t, err, &apiError)
				require.Equal(t, tc.code, apiError.ErrorCode())
			}
			require.Equal(t, []url.Values{{
				"Action": {"GetRole"}, "RoleName": {"lookup"}, "Version": {"2010-05-08"},
			}}, f.requests)
		})
	}
}

type emptyRoleGetter struct {
	requests []*iam.GetRoleInput
}

func (g *emptyRoleGetter) GetRole(
	_ context.Context, input *iam.GetRoleInput, _ ...func(*iam.Options),
) (*iam.GetRoleOutput, error) {
	g.requests = append(g.requests, input)
	return nil, nil
}

func TestRoleDataSourceRejectsNilResponse(t *testing.T) {
	g := &emptyRoleGetter{}
	got, err := (&RoleDataSource{RoleName: "missing"}).read(t.Context(), g)
	require.ErrorContains(t, err, `get role "missing": no role returned`)
	require.Nil(t, got)
	require.Equal(t, []*iam.GetRoleInput{{RoleName: aws.String("missing")}}, g.requests)
}

func getRoleResponse(role string) string {
	return "<GetRoleResponse><GetRoleResult>" + role + "</GetRoleResult></GetRoleResponse>"
}
