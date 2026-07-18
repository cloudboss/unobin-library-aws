package kinesis

// StreamResource manages one Kinesis data stream.
type StreamResource struct {
	Name                    string             `ub:"name"`
	EncryptionType          string             `ub:"encryption-type"`
	EnforceConsumerDeletion bool               `ub:"enforce-consumer-deletion"`
	KMSKeyID                *string            `ub:"kms-key-id"`
	MaxRecordSizeInKiB      *int64             `ub:"max-record-size-in-kib"`
	RetentionPeriod         int64              `ub:"retention-period"`
	ShardCount              *int64             `ub:"shard-count"`
	ShardLevelMetrics       *[]string          `ub:"shard-level-metrics"`
	StreamModeDetails       *StreamModeDetails `ub:"stream-mode-details"`
	Tags                    *map[string]string `ub:"tags"`
	WarmThroughputMiBps     *int64             `ub:"warm-throughput-mib-ps"`
}

type StreamModeDetails struct {
	StreamMode string `ub:"stream-mode"`
}

// StreamResourceOutput records the stream identity and values observed from Kinesis.
type StreamResourceOutput struct {
	ARN                     string             `ub:"arn"`
	Name                    string             `ub:"name"`
	OpenShardCount          int64              `ub:"open-shard-count"`
	StreamCreationTimestamp string             `ub:"stream-creation-timestamp"`
	StreamStatus            string             `ub:"stream-status"`
	ConsumerCount           *int64             `ub:"consumer-count"`
	MaxRecordSizeInKiB      *int64             `ub:"max-record-size-in-kib"`
	StreamModeDetails       *StreamModeDetails `ub:"stream-mode-details"`
	WarmThroughput          *WarmThroughput    `ub:"warm-throughput"`
}

type WarmThroughput struct {
	CurrentMiBps *int64 `ub:"current-mib-ps"`
	TargetMiBps  *int64 `ub:"target-mib-ps"`
}
