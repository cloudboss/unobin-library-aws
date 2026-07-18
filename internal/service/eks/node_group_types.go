package eks

// NodeGroupResource manages an Amazon EKS managed node group.
type NodeGroupResource struct {
	ClusterName           string                     `ub:"cluster-name"`
	NodeGroupName         string                     `ub:"node-group-name"`
	NodeRoleARN           string                     `ub:"node-role-arn"`
	SubnetIDs             []string                   `ub:"subnet-ids"`
	ScalingConfig         NodeGroupScalingConfig     `ub:"scaling-config"`
	AMIType               *string                    `ub:"ami-type"`
	CapacityType          *string                    `ub:"capacity-type"`
	DiskSize              *int64                     `ub:"disk-size"`
	InstanceTypes         *[]string                  `ub:"instance-types"`
	Labels                *map[string]string         `ub:"labels"`
	LaunchTemplateID      *string                    `ub:"launch-template-id"`
	LaunchTemplateName    *string                    `ub:"launch-template-name"`
	LaunchTemplateVersion *string                    `ub:"launch-template-version"`
	NodeRepairConfig      *NodeGroupNodeRepairConfig `ub:"node-repair-config"`
	ReleaseVersion        *string                    `ub:"release-version"`
	RemoteAccess          *NodeGroupRemoteAccess     `ub:"remote-access"`
	Tags                  *map[string]string         `ub:"tags"`
	Taints                *[]NodeGroupTaint          `ub:"taints"`
	UpdateConfig          *NodeGroupUpdateConfig     `ub:"update-config"`
	Version               *string                    `ub:"version"`
	ForceUpdateVersion    bool                       `ub:"force-update-version"`
	WarmPoolConfig        *NodeGroupWarmPoolConfig   `ub:"warm-pool-config"`
}

// NodeGroupResourceOutput holds node-group handles and cloud-selected values.
type NodeGroupResourceOutput struct {
	ClusterName                 string                     `ub:"cluster-name"`
	NodeGroupName               string                     `ub:"node-group-name"`
	ARN                         string                     `ub:"arn"`
	Status                      string                     `ub:"status"`
	AutoScalingGroupNames       []string                   `ub:"auto-scaling-group-names"`
	RemoteAccessSecurityGroupID *string                    `ub:"remote-access-security-group-id"`
	AmiType                     string                     `ub:"ami-type"`
	CapacityType                string                     `ub:"capacity-type"`
	DiskSize                    *int64                     `ub:"disk-size"`
	InstanceTypes               []string                   `ub:"instance-types"`
	LaunchTemplateId            *string                    `ub:"launch-template-id"`
	LaunchTemplateName          *string                    `ub:"launch-template-name"`
	LaunchTemplateVersion       *string                    `ub:"launch-template-version"`
	NodeRepairConfig            *NodeGroupNodeRepairConfig `ub:"node-repair-config"`
	ReleaseVersion              string                     `ub:"release-version"`
	UpdateConfig                *NodeGroupUpdateConfig     `ub:"update-config"`
	Version                     string                     `ub:"version"`
	WarmPoolConfig              *NodeGroupWarmPoolConfig   `ub:"warm-pool-config"`
}

type NodeGroupScalingConfig struct {
	DesiredSize int64 `ub:"desired-size"`
	MinSize     int64 `ub:"min-size"`
	MaxSize     int64 `ub:"max-size"`
}

type NodeGroupRemoteAccess struct {
	EC2SSHKey              string    `ub:"ec2-ssh-key"`
	SourceSecurityGroupIDs *[]string `ub:"source-security-group-ids"`
}

type NodeGroupTaint struct {
	Key    string  `ub:"key"`
	Value  *string `ub:"value"`
	Effect string  `ub:"effect"`
}

type NodeGroupUpdateConfig struct {
	MaxUnavailable           *int64  `ub:"max-unavailable"`
	MaxUnavailablePercentage *int64  `ub:"max-unavailable-percentage"`
	Strategy                 *string `ub:"strategy"`
}

type NodeGroupNodeRepairConfig struct {
	Enabled        *bool                          `ub:"enabled"`
	ParallelCount  *int64                         `ub:"max-parallel-nodes-repaired-count"`
	ParallelPct    *int64                         `ub:"max-parallel-nodes-repaired-percentage"`
	UnhealthyCount *int64                         `ub:"max-unhealthy-node-threshold-count"`
	UnhealthyPct   *int64                         `ub:"max-unhealthy-node-threshold-percentage"`
	Overrides      *[]NodeGroupNodeRepairOverride `ub:"overrides"`
}

type NodeGroupNodeRepairOverride struct {
	Condition         string `ub:"condition"`
	Reason            string `ub:"reason"`
	MinRepairWaitMins int64  `ub:"min-repair-wait-mins"`
	RepairAction      string `ub:"repair-action"`
}

type NodeGroupWarmPoolConfig struct {
	Enabled                  *bool   `ub:"enabled"`
	MaxGroupPreparedCapacity *int64  `ub:"max-group-prepared-capacity"`
	MinSize                  *int64  `ub:"min-size"`
	PoolState                *string `ub:"pool-state"`
	ReuseOnScaleIn           *bool   `ub:"reuse-on-scale-in"`
}
