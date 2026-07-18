package eks

// AddonResource manages an Amazon EKS add-on.
type AddonResource struct {
	ClusterName              string                         `ub:"cluster-name"`
	AddonName                string                         `ub:"addon-name"`
	AddonVersion             *string                        `ub:"addon-version"`
	ConfigurationValues      *string                        `ub:"configuration-values"`
	NamespaceConfig          *AddonNamespaceConfig          `ub:"namespace-config"`
	PodIdentityAssociation   *[]AddonPodIdentityAssociation `ub:"pod-identity-association"`
	Preserve                 bool                           `ub:"preserve"`
	ResolveConflictsOnCreate *string                        `ub:"resolve-conflicts-on-create"`
	ResolveConflictsOnUpdate *string                        `ub:"resolve-conflicts-on-update"`
	ServiceAccountRoleARN    *string                        `ub:"service-account-role-arn"`
	Tags                     *map[string]string             `ub:"tags"`
}

// AddonResourceOutput holds add-on identity and cloud-selected values.
type AddonResourceOutput struct {
	ClusterName  string  `ub:"cluster-name"`
	AddonName    string  `ub:"addon-name"`
	ARN          string  `ub:"arn"`
	AddonVersion string  `ub:"addon-version"`
	Namespace    *string `ub:"namespace"`
	Status       string  `ub:"status"`
	CreatedAt    string  `ub:"created-at"`
	ModifiedAt   string  `ub:"modified-at"`
}

type AddonNamespaceConfig struct {
	Namespace string `ub:"namespace"`
}

type AddonPodIdentityAssociation struct {
	RoleARN        string `ub:"role-arn"`
	ServiceAccount string `ub:"service-account"`
}
