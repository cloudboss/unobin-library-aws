package eks

type FargateProfileResource struct {
	ClusterName         string                   `ub:"cluster-name"`
	FargateProfileName  string                   `ub:"fargate-profile-name"`
	PodExecutionRoleARN string                   `ub:"pod-execution-role-arn"`
	Selector            []FargateProfileSelector `ub:"selector"`
	SubnetIDs           *[]string                `ub:"subnet-ids"`
	Tags                *map[string]string       `ub:"tags"`
}

type FargateProfileResourceOutput struct {
	ClusterName         string                   `ub:"cluster-name"`
	FargateProfileName  string                   `ub:"fargate-profile-name"`
	ARN                 string                   `ub:"arn"`
	PodExecutionRoleARN string                   `ub:"pod-execution-role-arn"`
	Selector            []FargateProfileSelector `ub:"selector"`
	Status              string                   `ub:"status"`
	SubnetIDs           []string                 `ub:"subnet-ids"`
}

type FargateProfileSelector struct {
	Namespace string             `ub:"namespace"`
	Labels    *map[string]string `ub:"labels"`
}
