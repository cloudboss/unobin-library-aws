package ec2

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/e2etest"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkACLCompileInputsAndOutputs(t *testing.T) {
	modules := t.TempDir()
	local := filepath.Join(modules, "local")
	require.NoError(t, os.Mkdir(local, 0o755))
	require.NoError(t, os.Symlink(libraryModuleRoot(t).Dir, filepath.Join(local, "aws")))
	e2etest.RunSourceCases(t, "testdata/ub/network-acl/compile",
		e2etest.WithSourceDirectory("modules", modules))
}

func TestNetworkACLCompiledCLIPlanEnvelopeAndScenario(t *testing.T) {
	module, unobin := libraryModuleRoot(t), unobinModuleRoot(t)
	for _, fixture := range []string{"literal", "scenario"} {
		t.Run(fixture, func(t *testing.T) {
			path := "testdata/ub/network-acl/compile/valid/factory.ub"
			if fixture == "scenario" {
				path = "../../tests/integration/scenarios/ec2-network-acl/factory.ub"
			}
			out := t.TempDir()
			result, err := compile.RunResult(compile.Options{
				FactoryPath: path, OutDir: out, Build: true,
				GoVersion: "1.26.2", CLIVersion: "v0.13.0", ReplaceUnobin: unobin.Dir,
				ReplaceGoModules: map[string]string{module.Path: module.Dir},
				NewResolver: func(directory string) (resolve.Resolver, error) {
					return resolve.NewLocalResolver(directory), nil
				}, Stdout: io.Discard, Stderr: io.Discard,
			})
			require.NoError(t, err)
			if fixture == "scenario" {
				return
			}
			config, err := os.ReadFile("testdata/ub/network-acl/compile/valid/config.ub")
			require.NoError(t, err)
			configPath := filepath.Join(out, "config.ub")
			require.NoError(t, os.WriteFile(configPath, config, 0o600))
			planPath := filepath.Join(out, "saved.plan")
			cmd := exec.CommandContext(t.Context(), result.BinaryPath, "plan", "--config", configPath,
				"--out", planPath, "--allow-version-mismatch")
			cmd.Dir = out
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, string(output))
			encoded, err := os.ReadFile(planPath)
			require.NoError(t, err)
			plan, err := runtime.OpenPlan(encoded, func(*runtime.StateRef) (encrypt.Encrypter, error) {
				return encrypters.Noop{}, nil
			})
			require.NoError(t, err)
			step := peeringRuntimeStep(t, plan)
			assert.Equal(t, runtime.DecisionCreate, step.Decision)
			literal := networkACLRuleInput()
			for _, field := range []string{"cidr-block", "ipv6-cidr-block", "icmp-type", "icmp-code"} {
				delete(literal, field)
			}
			assert.Equal(t, map[string]any{"vpc-id": "vpc-1",
				"ingress": []any{literal},
				"egress": []any{map[string]any{"rule-action": "DENY", "protocol": "-01",
					"rule-number": int64(32766), "from-port": int64(0), "to-port": int64(0)}},
				"subnet-ids": []any{"subnet-a", "subnet-a"},
				"tags":       map[string]any{"blank": ""}}, step.Inputs)
		})
	}
}
