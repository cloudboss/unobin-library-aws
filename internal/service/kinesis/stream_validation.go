package kinesis

import (
	"errors"
	"fmt"
	"math"
)

const (
	streamModeProvisioned = "PROVISIONED"
	streamModeOnDemand    = "ON_DEMAND"
	streamEncryptionNone  = "NONE"
	streamEncryptionKMS   = "KMS"
)

var streamMetricNames = map[string]struct{}{
	"IncomingBytes":                      {},
	"IncomingRecords":                    {},
	"OutgoingBytes":                      {},
	"OutgoingRecords":                    {},
	"WriteProvisionedThroughputExceeded": {},
	"ReadProvisionedThroughputExceeded":  {},
	"IteratorAgeMilliseconds":            {},
	"ALL":                                {},
}

func (r StreamResource) validate() error {
	if r.Name == "" {
		return errors.New("name must not be empty")
	}
	if r.EncryptionType != streamEncryptionNone && r.EncryptionType != streamEncryptionKMS {
		return errors.New("encryption-type must be NONE or KMS")
	}
	if r.EncryptionType == streamEncryptionKMS &&
		(r.KMSKeyID == nil || *r.KMSKeyID == "") {
		return errors.New("KMS encryption requires a non-empty kms-key-id")
	}
	if r.RetentionPeriod < 24 || r.RetentionPeriod > 8760 {
		return errors.New("retention-period must be between 24 and 8760")
	}
	if r.MaxRecordSizeInKiB != nil &&
		(*r.MaxRecordSizeInKiB < 1024 || *r.MaxRecordSizeInKiB > 10240) {
		return errors.New("max-record-size-in-kib must be between 1024 and 10240")
	}
	mode := effectiveStreamMode(r.StreamModeDetails)
	if mode != streamModeProvisioned && mode != streamModeOnDemand {
		return errors.New("stream-mode must be PROVISIONED or ON_DEMAND")
	}
	if mode == streamModeProvisioned {
		if r.ShardCount == nil {
			return errors.New("PROVISIONED mode requires shard-count")
		}
		if *r.ShardCount < 1 {
			return errors.New("shard-count must be at least 1")
		}
	}
	if mode == streamModeOnDemand && r.ShardCount != nil {
		return errors.New("ON_DEMAND mode forbids shard-count")
	}
	if r.WarmThroughputMiBps != nil && r.ShardCount != nil {
		return errors.New("warm-throughput-mib-ps conflicts with shard-count")
	}
	if err := validateStreamInt32("shard-count", r.ShardCount); err != nil {
		return err
	}
	if err := validateStreamInt32(
		"warm-throughput-mib-ps",
		r.WarmThroughputMiBps,
	); err != nil {
		return err
	}
	seen := make(map[string]struct{}, metricCount(r.ShardLevelMetrics))
	for _, metric := range stringSlice(r.ShardLevelMetrics) {
		if _, ok := streamMetricNames[metric]; !ok {
			return fmt.Errorf("unsupported shard-level metric %q", metric)
		}
		if _, ok := seen[metric]; ok {
			return fmt.Errorf("shard-level-metrics contains duplicate %q", metric)
		}
		seen[metric] = struct{}{}
	}
	return nil
}

func validateStreamInt32(name string, value *int64) error {
	if value != nil && (*value < math.MinInt32 || *value > math.MaxInt32) {
		return fmt.Errorf("%s must fit in a 32-bit integer", name)
	}
	return nil
}

func effectiveStreamMode(details *StreamModeDetails) string {
	if details == nil {
		return streamModeProvisioned
	}
	return details.StreamMode
}

func stringSlice(values *[]string) []string {
	if values == nil {
		return nil
	}
	return *values
}

func metricCount(values *[]string) int {
	if values == nil {
		return 0
	}
	return len(*values)
}

func equalStringSets(left, right *[]string) bool {
	leftValues := stringSlice(left)
	rightValues := stringSlice(right)
	if len(leftValues) != len(rightValues) {
		return false
	}
	counts := make(map[string]int, len(leftValues))
	for _, value := range leftValues {
		counts[value]++
	}
	for _, value := range rightValues {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	return true
}

func equalStringMaps(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
