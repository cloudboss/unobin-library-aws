package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("verify: %v", err)
	}
}

func run() error {
	switch mode := os.Getenv("VERIFY_PHASE"); mode {
	case "applied":
		return verifyPlan("plan.json", "us-east-1", "ec2")
	case "updated":
		return verifyPlan("plan-update.json", "us-east-2", "s3")
	case "destroyed":
		fmt.Println("ok: metadata scenario has no cloud resources")
		return nil
	default:
		return fmt.Errorf("VERIFY_PHASE must be applied, updated, or destroyed, got %q", mode)
	}
}

func verifyPlan(fileName, namedRegion, serviceID string) error {
	buildDir := os.Getenv("VERIFY_BUILD_DIR")
	if buildDir == "" {
		return errors.New("VERIFY_BUILD_DIR is required")
	}
	plan, err := readPlan(filepath.Join(buildDir, fileName))
	if err != nil {
		return err
	}
	steps := dataSteps(plan)
	if err := checkARN(steps); err != nil {
		return err
	}
	if err := checkIPRanges(steps); err != nil {
		return err
	}
	if err := checkPartition(steps); err != nil {
		return err
	}
	if err := checkCurrentRegion(steps); err != nil {
		return err
	}
	if err := checkNamedRegion(steps, namedRegion); err != nil {
		return err
	}
	if err := checkRegions(steps); err != nil {
		return err
	}
	if err := checkService(steps, namedRegion, serviceID); err != nil {
		return err
	}
	if err := checkServicePrincipal(steps); err != nil {
		return err
	}
	fmt.Printf("ok: metadata outputs verified in %s\n", fileName)
	return nil
}

type envelope struct {
	Ciphertext string `json:"ciphertext"`
}

type planFile struct {
	Steps []planStep `json:"steps"`
}

type planStep struct {
	Address         string         `json:"address"`
	NodeKind        string         `json:"node-kind"`
	Decision        string         `json:"decision"`
	ObservedOutputs map[string]any `json:"observed-outputs"`
}

func readPlan(path string) (*planFile, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode plan envelope: %w", err)
	}
	plain, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode plan body: %w", err)
	}
	var plan planFile
	if err := json.Unmarshal(plain, &plan); err != nil {
		return nil, fmt.Errorf("decode plan body JSON: %w", err)
	}
	return &plan, nil
}

func dataSteps(plan *planFile) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, step := range plan.Steps {
		if step.NodeKind == "data-source" && step.Decision == "read" {
			out[step.Address] = step.ObservedOutputs
		}
	}
	return out
}

func checkARN(steps map[string]map[string]any) error {
	out, err := requiredStep(steps, "data-source.arn-it")
	if err != nil {
		return err
	}
	checks := map[string]string{
		"account":   "123456789012",
		"partition": "aws",
		"region":    "us-east-1",
		"resource":  "function:unobin-it",
		"service":   "lambda",
	}
	return checkStrings("arn", out, checks)
}

func checkIPRanges(steps map[string]map[string]any) error {
	out, err := requiredStep(steps, "data-source.ip-ranges-it")
	if err != nil {
		return err
	}
	if got := stringList(out, "cidr-blocks"); len(got) == 0 {
		return errors.New("ip-ranges cidr-blocks is empty")
	}
	if got := stringValue(out, "create-date"); got == "" {
		return errors.New("ip-ranges create-date is empty")
	}
	if got := numberValue(out, "sync-token"); got <= 0 {
		return fmt.Errorf("ip-ranges sync-token is %v", got)
	}
	return nil
}

func checkPartition(steps map[string]map[string]any) error {
	out, err := requiredStep(steps, "data-source.partition-it")
	if err != nil {
		return err
	}
	if got := stringValue(out, "partition"); got == "" {
		return errors.New("partition is empty")
	}
	if got := stringValue(out, "dns-suffix"); got == "" {
		return errors.New("partition dns-suffix is empty")
	}
	if got := stringValue(out, "reverse-dns-prefix"); got == "" {
		return errors.New("partition reverse-dns-prefix is empty")
	}
	return nil
}

func checkCurrentRegion(steps map[string]map[string]any) error {
	out, err := requiredStep(steps, "data-source.current-region-it")
	if err != nil {
		return err
	}
	if got := stringValue(out, "region"); got == "" {
		return errors.New("current region is empty")
	}
	if got := stringValue(out, "endpoint"); got == "" {
		return errors.New("current region endpoint is empty")
	}
	return nil
}

func checkNamedRegion(steps map[string]map[string]any, namedRegion string) error {
	out, err := requiredStep(steps, "data-source.named-region-it")
	if err != nil {
		return err
	}
	checks := map[string]string{
		"endpoint":  fmt.Sprintf("ec2.%s.amazonaws.com", namedRegion),
		"partition": "aws",
		"region":    namedRegion,
	}
	return checkStrings("named region", out, checks)
}

func checkRegions(steps map[string]map[string]any) error {
	out, err := requiredStep(steps, "data-source.regions-it")
	if err != nil {
		return err
	}
	if got := stringList(out, "names"); len(got) == 0 {
		return errors.New("regions names is empty")
	}
	if got := stringValue(out, "partition"); got == "" {
		return errors.New("regions partition is empty")
	}
	return nil
}

func checkService(steps map[string]map[string]any, namedRegion, serviceID string) error {
	out, err := requiredStep(steps, "data-source.service-it")
	if err != nil {
		return err
	}
	checks := map[string]string{
		"dns-name":  fmt.Sprintf("%s.%s.amazonaws.com", serviceID, namedRegion),
		"partition": "aws",
		"region":    namedRegion,
		"reverse-dns-name": fmt.Sprintf("com.amazonaws.%s.%s", namedRegion,
			serviceID),
		"reverse-dns-prefix": "com.amazonaws",
		"service-id":         serviceID,
	}
	if err := checkStrings("service", out, checks); err != nil {
		return err
	}
	if got, ok := out["supported"].(bool); !ok || !got {
		return fmt.Errorf("service supported is %v", out["supported"])
	}
	return nil
}

func checkServicePrincipal(steps map[string]map[string]any) error {
	out, err := requiredStep(steps, "data-source.service-principal-it")
	if err != nil {
		return err
	}
	checks := map[string]string{
		"name":   "logs.amazonaws.com.cn",
		"region": "cn-north-1",
		"suffix": "amazonaws.com.cn",
	}
	return checkStrings("service principal", out, checks)
}

func requiredStep(steps map[string]map[string]any, address string) (map[string]any, error) {
	out, ok := steps[address]
	if !ok {
		return nil, fmt.Errorf("missing data step %s", address)
	}
	return out, nil
}

func checkStrings(label string, out map[string]any, checks map[string]string) error {
	for key, want := range checks {
		if got := stringValue(out, key); got != want {
			return fmt.Errorf("%s %s is %q, want %q", label, key, got, want)
		}
	}
	return nil
}

func stringValue(out map[string]any, key string) string {
	got, _ := out[key].(string)
	return got
}

func stringList(out map[string]any, key string) []string {
	values, ok := out[key].([]any)
	if !ok {
		return nil
	}
	outValues := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			return nil
		}
		outValues = append(outValues, text)
	}
	return outValues
}

func numberValue(out map[string]any, key string) float64 {
	got, _ := out[key].(float64)
	return got
}
