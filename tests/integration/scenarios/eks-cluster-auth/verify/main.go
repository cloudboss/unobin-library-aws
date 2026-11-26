package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/state/local"
)

func main() {
	if err := verify(); err != nil {
		log.Fatal(err)
	}
}

func verify() error {
	build, phase := os.Getenv("VERIFY_BUILD_DIR"), os.Getenv("VERIFY_PHASE")
	if phase != "applied" && phase != "updated" && phase != "destroyed" {
		return fmt.Errorf("unexpected phase %q", phase)
	}
	store, err := local.NewStore(filepath.Join(build, ".unobin", "state"),
		"eks-cluster-auth", "config", encrypters.Noop{})
	if err != nil {
		return err
	}
	snapshot, err := store.Current()
	if err != nil {
		return err
	}
	entry := snapshot.Find("action.auth")
	if phase == "destroyed" {
		if entry != nil {
			return errors.New("action state remains after destruction")
		}
		return nil
	}
	if entry == nil || !slices.Equal(entry.SensitiveOutputs, []string{"token"}) {
		return errors.New("sensitive action output absent")
	}
	token, ok := entry.Outputs["token"].(string)
	if !ok || !strings.HasPrefix(token, "k8s-aws-v1.") {
		return errors.New("token prefix differs")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "k8s-aws-v1."))
	if err != nil {
		return err
	}
	signed, err := url.Parse(string(decoded))
	if err != nil {
		return err
	}
	if signed.Query().Get("X-Amz-Expires") != "0" ||
		signed.Query().Get("X-Amz-SignedHeaders") != "host;x-k8s-aws-id" ||
		signed.Query().Get("Action") != "GetCallerIdentity" ||
		entry.Inputs["name"] != "unobin-nonexistent-cluster" {
		return errors.New("signed request differs")
	}
	endpoint := os.Getenv("AWS_ENDPOINT_URL_STS")
	if endpoint == "" {
		endpoint = os.Getenv("AWS_ENDPOINT_URL")
	}
	if endpoint != "" {
		expected, err := url.Parse(endpoint)
		if err != nil {
			return err
		}
		if signed.Host != expected.Host {
			return errors.New("STS endpoint differs")
		}
	}
	planName := "plan.json"
	if phase == "updated" {
		planName = "plan-update.json"
	}
	encoded, err := os.ReadFile(filepath.Join(build, planName))
	if err != nil {
		return err
	}
	plan, err := runtime.OpenPlan(encoded, func(*runtime.StateRef) (encrypt.Encrypter, error) {
		return encrypters.Noop{}, nil
	})
	if err != nil {
		return err
	}
	for _, step := range plan.Steps {
		if step.Address == "action.auth" && step.Decision == runtime.DecisionRerun {
			fmt.Println("ok: locally signed token persisted; apply scheduled generation")
			return nil
		}
	}
	return errors.New("token generation was not scheduled")
}
