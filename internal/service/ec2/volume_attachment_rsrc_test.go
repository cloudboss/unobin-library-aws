package ec2

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type attachmentCall struct {
	form   url.Values
	status int
	body   string
}

func runAttachmentScript(t *testing.T, script ...attachmentCall) (*awsCfg, volumeAttachmentClient) {
	t.Helper()
	fake := newFakeEC2(t)
	want := make([]url.Values, 0, len(script))
	actions := make(map[string][]attachmentCall)
	for _, step := range script {
		action := step.form.Get("Action")
		actions[action] = append(actions[action], step)
		want = append(want, step.form)
	}
	for action, steps := range actions {
		fake.on(action, func(n int, _ url.Values) (int, string) {
			if n > len(steps) {
				t.Errorf("unexpected %s request %d", action, n)
				return 400, ec2ErrorXML("InvalidAction", "unexpected request")
			}
			return steps[n-1].status, steps[n-1].body
		})
	}
	t.Cleanup(func() {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if len(want) == 0 {
			assert.Empty(t, fake.requests)
		} else {
			assert.Equal(t, want, fake.requests)
		}
	})
	cfg := fake.configuration()
	client, err := newClient(t.Context(), cfg)
	require.NoError(t, err)
	return cfg, volumeAttachmentClient{ec2: client, pollInterval: time.Microsecond}
}

var testAttachment = VolumeAttachmentResource{
	DeviceName: "/dev/sdf", InstanceId: "i-one", VolumeId: "vol-one",
}

func attachmentForm(action string) url.Values {
	return url.Values{"Action": {action}, "Version": {"2016-11-15"}}
}

func volumePage(tkn string, volumes ...string) string {
	return "<DescribeVolumesResponse><volumeSet>" + strings.Join(volumes, "") +
		"</volumeSet><nextToken>" + tkn + "</nextToken></DescribeVolumesResponse>"
}

func volumeXML(id, state string, attachments ...string) string {
	return "<item><volumeId>" + id + "</volumeId><status>" + state +
		"</status><attachmentSet>" + strings.Join(attachments, "") + "</attachmentSet></item>"
}

func attachmentXML(device, instance, state string) string {
	return "<item><device>" + device + "</device><instanceId>" + instance +
		"</instanceId><status>" + state + "</status></item>"
}

func volumeDescribe(device, instance, volume, token string) url.Values {
	form := attachmentForm("DescribeVolumes")
	form.Set("VolumeId.1", volume)
	filter := 1
	for _, field := range []struct{ name, value string }{
		{"attachment.device", device}, {"attachment.instance-id", instance},
	} {
		if field.value != "" {
			prefix := "Filter." + strconv.Itoa(filter)
			form.Set(prefix+".Name", field.name)
			form.Set(prefix+".Value.1", field.value)
			filter++
		}
	}
	if token != "" {
		form.Set("NextToken", token)
	}
	return form
}

func attachmentDescribe(body string) attachmentCall {
	return attachmentCall{volumeDescribe("/dev/sdf", "i-one", "vol-one", ""), 200, body}
}

func instanceXML(state, reason string) string {
	return "<item><instancesSet><item><instanceId>i-one</instanceId>" +
		state + reason + "</item></instancesSet></item>"
}

func instancePage(token string, reservations ...string) string {
	return "<DescribeInstancesResponse><reservationSet>" + strings.Join(reservations, "") +
		"</reservationSet><nextToken>" + token + "</nextToken></DescribeInstancesResponse>"
}

func instanceDescribe(token, body string) attachmentCall {
	form := attachmentForm("DescribeInstances")
	form.Set("InstanceId.1", "i-one")
	if token != "" {
		form.Set("NextToken", token)
	}
	return attachmentCall{form, 200, body}
}

func instanceStateCall(state string) attachmentCall {
	return instanceDescribe("", instancePage("", instanceXML(
		"<instanceState><name>"+state+"</name></instanceState>", "",
	)))
}

func stopAttachmentInstance() attachmentCall {
	form := attachmentForm("StopInstances")
	form.Set("InstanceId.1", "i-one")
	form.Set("Force", "false")
	return attachmentCall{form, 200, "<StopInstancesResponse/>"}
}

func changeAttachment(action, device, force string) attachmentCall {
	form := attachmentForm(action)
	form.Set("Device", device)
	form.Set("InstanceId", "i-one")
	form.Set("VolumeId", "vol-one")
	if force != "" {
		form.Set("Force", force)
	}
	return attachmentCall{form, 200, "<" + action + "Response/>"}
}

func withAttachmentError(call attachmentCall, code, message string) attachmentCall {
	call.status = 400
	call.body = ec2ErrorXML(code, message)
	return call
}

func attachmentStatus(state string) attachmentCall {
	return attachmentDescribe(volumePage("", volumeXML("vol-one", "in-use",
		attachmentXML("/dev/sdf", "i-one", state))))
}

func TestVolumeAttachmentLookup(t *testing.T) {
	match := volumeXML("vol-one", "in-use",
		attachmentXML("/dev/sdg", "i-one", "busy"),
		attachmentXML("/dev/sdf", "i-two", "busy"),
		attachmentXML("/dev/sdf", "i-one", "detached"),
		attachmentXML("/dev/sdf", "i-one", "attaching"),
		attachmentXML("/dev/sdf", "i-one", "attached"))
	for _, tc := range []struct {
		name    string
		steps   []attachmentCall
		state   ec2types.VolumeAttachmentState
		failure string
	}{
		{"exact and first across pages", []attachmentCall{
			attachmentDescribe(volumePage("page-two")),
			{volumeDescribe("/dev/sdf", "i-one", "vol-one", "page-two"), 200, volumePage("", match)},
		}, ec2types.VolumeAttachmentStateAttaching, ""},
		{"missing volume code", []attachmentCall{
			withAttachmentError(attachmentStatus("attached"), "InvalidVolume.NotFound", "gone"),
		}, "", ""},
		{"zero volumes", []attachmentCall{attachmentDescribe(volumePage(""))}, "", ""},
		{"multiple volumes", []attachmentCall{
			attachmentDescribe(volumePage("page-two", match)),
			{volumeDescribe("/dev/sdf", "i-one", "vol-one", "page-two"), 200, volumePage("", match)},
		}, "", ""},
		{"available", []attachmentCall{attachmentDescribe(volumePage("", volumeXML(
			"vol-one", "available", attachmentXML("/dev/sdf", "i-one", "attached"),
		)))}, "", ""},
		{"deleted", []attachmentCall{attachmentDescribe(volumePage("", volumeXML(
			"vol-one", "deleted", attachmentXML("/dev/sdf", "i-one", "attached"),
		)))}, "", ""},
		{"wrong volume", []attachmentCall{attachmentDescribe(volumePage("", volumeXML(
			"vol-other", "in-use", attachmentXML("/dev/sdf", "i-one", "attached"),
		)))}, "", ""},
		{"only decoys and detached", []attachmentCall{attachmentDescribe(volumePage("", volumeXML(
			"vol-one", "in-use", attachmentXML("/dev/sdf", "i-two", "attached"),
			attachmentXML("/dev/sdg", "i-one", "attached"),
			attachmentXML("/dev/sdf", "i-one", "detached"),
		)))}, "", ""},
		{"later-page error", []attachmentCall{
			attachmentDescribe(volumePage("page-two", match)),
			{volumeDescribe("/dev/sdf", "i-one", "vol-one", "page-two"), 400,
				ec2ErrorXML("UnauthorizedOperation", "cannot read later page")},
		}, "", "UnauthorizedOperation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, client := runAttachmentScript(t, tc.steps...)
			got, err := testAttachment.lookup(t.Context(), client.ec2)
			assert.Equal(t, tc.state, got)
			switch {
			case tc.failure != "":
				require.ErrorContains(t, err, tc.failure)
				assert.NotErrorIs(t, err, runtime.ErrNotFound)
			case tc.state == "":
				require.ErrorIs(t, err, runtime.ErrNotFound)
			default:
				require.NoError(t, err)
			}
		})
	}
}

func TestVolumeAttachmentCreate(t *testing.T) {
	absent := attachmentDescribe(volumePage(""))
	attached := attachmentStatus("attached")
	attaching := attachmentStatus("attaching")
	attach := changeAttachment("AttachVolume", "/dev/sdf", "")
	for _, tc := range []struct {
		name, failure string
		steps         []attachmentCall
	}{
		{"fresh attach", "", []attachmentCall{
			absent, instanceStateCall("pending"), instanceStateCall("stopping"),
			instanceStateCall("running"), attach, attaching, attached, attached,
		}},
		{"adopt attaching", "", []attachmentCall{attaching, attaching, attached, attached}},
		{"adopt attached", "", []attachmentCall{attached, attached, attached}},
		{"failed initial lookup", "UnauthorizedOperation", []attachmentCall{
			withAttachmentError(attached, "UnauthorizedOperation", "no read access"),
		}},
		{"attach error", "IncorrectState", []attachmentCall{
			absent, instanceStateCall("stopped"), withAttachmentError(attach, "IncorrectState", "busy"),
		}},
		{"lost at final lookup", "confirm attached volume", []attachmentCall{
			attached, attached, absent,
		}},
		{"terminated before attach", "unexpected state \"terminated\": spot instance reclaimed",
			[]attachmentCall{absent, instanceDescribe("", instancePage("", instanceXML(
				"<instanceState><name>terminated</name></instanceState>",
				"<stateReason><code>Server.Spot</code><message>spot instance reclaimed</message></stateReason>",
			)))}},
		{"unexpected attachment state", "unexpected state \"busy\"",
			[]attachmentCall{attached, attachmentStatus("busy")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, client := runAttachmentScript(t, tc.steps...)
			output, err := testAttachment.create(t.Context(), client)
			if tc.failure != "" {
				require.ErrorContains(t, err, tc.failure)
				assert.Nil(t, output)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, &VolumeAttachmentResourceOutput{Region: "us-east-1"}, output)
		})
	}
}

func TestVolumeAttachmentEmptyDevicePassesToEC2(t *testing.T) {
	r := testAttachment
	r.DeviceName = ""
	read := volumeDescribe("", "i-one", "vol-one", "")
	attached := attachmentCall{read, 200, volumePage("", volumeXML(
		"vol-one", "in-use", attachmentXML("", "i-one", "attached"),
	))}
	_, client := runAttachmentScript(t,
		attachmentCall{read, 200, volumePage("")}, instanceStateCall("running"),
		changeAttachment("AttachVolume", "", ""), attached, attached)
	output, err := r.create(t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, &VolumeAttachmentResourceOutput{Region: "us-east-1"}, output)
}

func TestVolumeAttachmentInstanceLookup(t *testing.T) {
	running := instanceXML("<instanceState><name>running</name></instanceState>",
		"<stateReason><code>Server.Reason</code></stateReason>")
	for _, tc := range []struct {
		name    string
		steps   []attachmentCall
		wantErr string
	}{
		{"single instance across pages", []attachmentCall{
			instanceDescribe("", instancePage("page-two")),
			instanceDescribe("page-two", instancePage("", running)),
		}, ""},
		{"no instances", []attachmentCall{instanceDescribe("", instancePage(""))}, "missing"},
		{"no state", []attachmentCall{
			instanceDescribe("", instancePage("", instanceXML("", ""))),
		}, "missing"},
		{"multiple instances", []attachmentCall{
			instanceDescribe("", instancePage("page-two", running)),
			instanceDescribe("page-two", instancePage("", running)),
		}, "missing"},
		{"not found from API", []attachmentCall{
			withAttachmentError(instanceStateCall("running"), "InvalidInstanceID.NotFound", "gone"),
		}, "missing"},
		{"later-page error", []attachmentCall{
			instanceDescribe("", instancePage("page-two", running)),
			withAttachmentError(instanceDescribe("page-two", ""), "UnauthorizedOperation", "denied"),
		}, "UnauthorizedOperation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, client := runAttachmentScript(t, tc.steps...)
			state, reason, err := testAttachment.instanceState(t.Context(), client.ec2)
			switch tc.wantErr {
			case "missing":
				require.ErrorIs(t, err, runtime.ErrNotFound)
			case "":
				require.NoError(t, err)
				assert.Equal(t, ec2types.InstanceStateNameRunning, state)
				assert.Equal(t, "Server.Reason", reason)
			default:
				require.ErrorContains(t, err, tc.wantErr)
				assert.NotErrorIs(t, err, runtime.ErrNotFound)
			}
		})
	}
}

func TestVolumeAttachmentDelete(t *testing.T) {
	r := testAttachment
	absent := attachmentDescribe(volumePage(""))
	detach := changeAttachment("DetachVolume", "/dev/sdf", "false")
	forced := changeAttachment("DetachVolume", "/dev/sdf", "true")
	stop := stopAttachmentInstance()
	for _, tc := range []struct {
		name, failure     string
		force, skip, stop *bool
		steps             []attachmentCall
	}{
		{"unset flags", "", nil, nil, nil, []attachmentCall{detach, absent}},
		{"retain before loading configuration", "", aws.Bool(true), aws.Bool(true), aws.Bool(true), nil},
		{"stop before force detach", "", aws.Bool(true), nil, aws.Bool(true), []attachmentCall{
			stop, instanceStateCall("pending"), instanceStateCall("running"),
			instanceStateCall("shutting-down"), instanceStateCall("stopping"),
			instanceStateCall("stopped"), forced, attachmentStatus("detaching"), absent,
		}},
		{"stop API error", "OperationNotPermitted", nil, nil, aws.Bool(true), []attachmentCall{
			withAttachmentError(stop, "OperationNotPermitted", "cannot stop"),
		}},
		{"stop wait error", "unexpected state \"terminated\"", nil, nil, aws.Bool(true),
			[]attachmentCall{stop, instanceStateCall("terminated")}},
		{"available means already detached", "", nil, nil, nil, []attachmentCall{
			withAttachmentError(detach, "IncorrectState", "volume is available"),
		}},
		{"available requires exact case", "IncorrectState", nil, nil, nil, []attachmentCall{
			withAttachmentError(detach, "IncorrectState", "volume is Available"),
		}},
		{"other incorrect state", "IncorrectState", nil, nil, nil, []attachmentCall{
			withAttachmentError(detach, "IncorrectState", "volume is busy"),
		}},
		{"other availability error code", "InvalidParameterValue", nil, nil, nil, []attachmentCall{
			withAttachmentError(detach, "InvalidParameterValue", "volume is available"),
		}},
		{"direct missing volume propagates", "InvalidVolume.NotFound", nil, nil, nil, []attachmentCall{
			withAttachmentError(detach, "InvalidVolume.NotFound", "gone"),
		}},
		{"missing volume while polling succeeds", "", nil, nil, nil, []attachmentCall{
			detach, withAttachmentError(absent, "InvalidVolume.NotFound", "gone"),
		}},
		{"unexpected detach state", "unexpected state \"attached\"", nil, nil, nil,
			[]attachmentCall{detach, attachmentStatus("attached")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, client := runAttachmentScript(t, tc.steps...)
			r.ForceDetach, r.SkipDestroy, r.StopInstanceBeforeDetaching = tc.force, tc.skip, tc.stop
			var err error
			if aws.ToBool(tc.skip) {
				err = (&VolumeAttachmentResource{}).Delete(t.Context(), &awsCfg{
					Region: aws.String(""), EndpointURL: aws.String("://bad endpoint"),
				}, volumeAttachmentPrior{Inputs: r})
			} else {
				err = r.delete(t.Context(), client)
			}
			if tc.failure != "" {
				require.ErrorContains(t, err, tc.failure)
				assert.NotErrorIs(t, err, runtime.ErrNotFound)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestVolumeAttachmentWaitMissingBudget(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(fmt.Sprintf("reset=%v", reset), func(t *testing.T) {
			missing := ""
			sequence := slices.Repeat([]string{missing}, 20)
			observed := append(slices.Clone(sequence), missing)
			if reset {
				sequence = append(sequence, "attaching")
				sequence = append(sequence, slices.Repeat([]string{missing}, 20)...)
				sequence = append(sequence, "attached")
				observed = slices.Clone(sequence)
			} else {
				sequence = append(sequence, missing, "attached")
			}
			var got []string
			err := waitForVolumeAttachmentState(t.Context(), "volume", time.Second, 0,
				func(context.Context) (string, string, error) {
					state := sequence[len(got)]
					got = append(got, state)
					if state == missing {
						return "", "", runtime.ErrNotFound
					}
					return state, "", nil
				}, []string{"attaching"}, []string{"attached"})
			if reset {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "21 consecutive observations")
			}
			assert.Equal(t, observed, got)
		})
	}
}

func TestVolumeAttachmentWaitBounds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		want    error
	}{
		{"phase deadline", 20 * time.Millisecond, context.DeadlineExceeded},
		{"caller cancellation", time.Minute, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			err := waitForVolumeAttachmentState(ctx, "volume", tc.timeout, time.Minute,
				func(ctx context.Context) (string, string, error) {
					if tc.want == context.Canceled {
						cancel()
					}
					<-ctx.Done()
					return "", "", ctx.Err()
				}, []string{"attaching"}, []string{"attached"})
			require.ErrorIs(t, err, tc.want)
		})
	}
}
