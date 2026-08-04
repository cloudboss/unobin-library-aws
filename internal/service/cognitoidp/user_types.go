package cognitoidp

type UserResource struct {
	UserPoolID             string             `ub:"user-pool-id"`
	Username               string             `ub:"username"`
	Attributes             *map[string]string `ub:"attributes"`
	ClientMetadata         *map[string]string `ub:"client-metadata"`
	ValidationData         *map[string]string `ub:"validation-data"`
	DesiredDeliveryMediums *[]string          `ub:"desired-delivery-mediums"`
	Enabled                *bool              `ub:"enabled"`
	ForceAliasCreation     *bool              `ub:"force-alias-creation"`
	MessageAction          *string            `ub:"message-action"`
	TemporaryPassword      *string            `ub:"temporary-password,sensitive"`
	Password               *string            `ub:"password,sensitive"`
}

type UserResourceOutput struct {
	Attributes          map[string]string `ub:"attributes"`
	CreationDate        string            `ub:"creation-date"`
	LastModifiedDate    string            `ub:"last-modified-date"`
	Enabled             bool              `ub:"enabled"`
	MFASettingList      []string          `ub:"mfa-setting-list"`
	PreferredMFASetting string            `ub:"preferred-mfa-setting"`
	Status              string            `ub:"status"`
	Sub                 string            `ub:"sub"`
	UserPoolID          string            `ub:"user-pool-id"`
	Username            string            `ub:"username"`
}
