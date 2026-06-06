package ec2

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cloudboss/unobin/pkg/defaults"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// KeyPair is an EC2 key pair imported from an existing public key. Create runs
// ImportKeyPair: EC2 stores the public key under a name and returns the key
// pair's computed id, fingerprint, and type. There is no in-place change to the
// key material or name, so the name and the public key are fixed once the key
// pair exists and a change to either replaces it. The only update is the tag
// set, reconciled through the EC2 tag calls keyed on the key pair id. The key
// name is at most 255 bytes, which EC2 enforces; no other constraint applies.
type KeyPair struct {
	KeyName   string            `ub:"key-name"`
	PublicKey string            `ub:"public-key"`
	Tags      map[string]string `ub:"tags"`
}

// KeyPairOutput holds the values EC2 computes for a key pair. The id is the
// stable handle the tag calls address. The fingerprint is the MD5 digest of the
// imported key's DER encoding, which EC2 returns and the read stores. The type
// is the key algorithm EC2 inferred from the imported material.
type KeyPairOutput struct {
	KeyPairId   string `ub:"key-pair-id"`
	Fingerprint string `ub:"fingerprint"`
	KeyType     string `ub:"key-type"`
}

func (r *KeyPair) SchemaVersion() int { return 1 }

// ReplaceFields lists the inputs EC2 fixes when a key pair is imported. The name
// identifies the key pair and the public key is the stored material; neither can
// change on an existing key pair, so a change to either requires a new one.
func (r *KeyPair) ReplaceFields() []string {
	return []string{"key-name", "public-key"}
}

// Defaults marks the tag map a key pair may omit.
func (r KeyPair) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Optional(r.Tags),
	}
}

func (r *KeyPair) Create(ctx context.Context, cfg any) (*KeyPairOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	in := &ec2.ImportKeyPairInput{
		KeyName:           aws.String(r.KeyName),
		PublicKeyMaterial: []byte(r.PublicKey),
		TagSpecifications: tagSpecifications(ec2types.ResourceTypeKeyPair, r.Tags),
	}
	if _, err := client.ImportKeyPair(ctx, in); err != nil {
		return nil, fmt.Errorf("import key pair: %w", err)
	}
	// ImportKeyPair returns the id and fingerprint, but the computed key type is
	// only on the describe form, so read by name for the full set of outputs.
	return r.read(ctx, client)
}

func (r *KeyPair) Read(ctx context.Context, cfg any, prior *KeyPairOutput) (*KeyPairOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.read(ctx, client)
}

// read fetches the key pair by name and returns its computed outputs. The name
// is the lookup handle and is fixed for the life of the key pair, so it comes
// from the input rather than a prior output. EC2 reports a missing key pair by
// service code on an HTTP 400, never a 404, so the not-found code maps to
// runtime.ErrNotFound; an empty result means the same. A describe that returns a
// record under a different name is a stale read of a just-replaced key pair, so
// it too maps to ErrNotFound rather than satisfying the read with the wrong key.
func (r *KeyPair) read(ctx context.Context, client *ec2.Client) (*KeyPairOutput, error) {
	resp, err := client.DescribeKeyPairs(ctx, &ec2.DescribeKeyPairsInput{
		KeyNames: []string{r.KeyName},
	})
	if err != nil {
		if isNotFound(err, "InvalidKeyPair.NotFound") {
			return nil, runtime.ErrNotFound
		}
		return nil, fmt.Errorf("describe key pairs: %w", err)
	}
	if len(resp.KeyPairs) == 0 {
		return nil, runtime.ErrNotFound
	}
	if len(resp.KeyPairs) > 1 {
		return nil, fmt.Errorf("describe key pairs: %d records for name %q",
			len(resp.KeyPairs), r.KeyName)
	}
	kp := resp.KeyPairs[0]
	if aws.ToString(kp.KeyName) != r.KeyName {
		return nil, runtime.ErrNotFound
	}
	return &KeyPairOutput{
		KeyPairId:   aws.ToString(kp.KeyPairId),
		Fingerprint: aws.ToString(kp.KeyFingerprint),
		KeyType:     string(kp.KeyType),
	}, nil
}

func (r *KeyPair) Update(
	ctx context.Context, cfg any, prior runtime.Prior[KeyPair, *KeyPairOutput],
) (*KeyPairOutput, error) {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// A key pair has no mutable field other than tags; the name and public key
	// are replace-only. Reconcile the tag set against the key pair id whenever it
	// changed, the same as the other EC2 resources.
	if runtime.Changed(prior.Inputs.Tags, r.Tags) {
		if err := syncTags(ctx, client, prior.Outputs.KeyPairId, r.Tags); err != nil {
			return nil, err
		}
		return r.read(ctx, client)
	}
	return prior.Outputs, nil
}

func (r *KeyPair) Delete(ctx context.Context, cfg any, prior *KeyPairOutput) error {
	client, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	// EC2 deletes a key pair by name and reports success even when none exists,
	// so no not-found tolerance is needed here; any other error propagates.
	_, err = client.DeleteKeyPair(ctx, &ec2.DeleteKeyPairInput{
		KeyName: aws.String(r.KeyName),
	})
	if err != nil {
		return fmt.Errorf("delete key pair: %w", err)
	}
	return nil
}
