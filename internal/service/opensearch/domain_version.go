package opensearch

func effectiveDomainEngineVersion(
	desired *string,
	observed,
	outputs *DomainResourceOutput,
) *string {
	if desired != nil {
		return desired
	}
	if observed != nil && observed.EngineVersion != "" {
		value := observed.EngineVersion
		return &value
	}
	if outputs != nil && outputs.EngineVersion != "" {
		value := outputs.EngineVersion
		return &value
	}
	return nil
}
