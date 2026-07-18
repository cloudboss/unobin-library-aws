package opensearch

import "reflect"

func equivalentDomainVPCOptions(prior, current *DomainVPCOptions) bool {
	if prior == nil || current == nil {
		return prior == nil && current == nil
	}
	return reflect.DeepEqual(prior.EgressEnabled, current.EgressEnabled) &&
		optionalUnorderedDomainSliceEqual(
			prior.SecurityGroupIDs,
			current.SecurityGroupIDs,
		) && optionalUnorderedDomainSliceEqual(prior.SubnetIDs, current.SubnetIDs)
}

func equivalentDomainAutoTuneOptions(prior, current *DomainAutoTuneOptions) bool {
	if prior == nil || current == nil {
		return prior == nil && current == nil
	}
	return prior.DesiredState == current.DesiredState &&
		reflect.DeepEqual(prior.RollbackOnDisable, current.RollbackOnDisable) &&
		prior.UseOffPeakWindow == current.UseOffPeakWindow &&
		optionalUnorderedDomainSliceEqual(
			prior.MaintenanceSchedule,
			current.MaintenanceSchedule,
		)
}

func equivalentDomainLogPublishingOptions(
	prior,
	current *[]DomainLogPublishingOption,
) bool {
	return optionalUnorderedDomainSliceEqual(prior, current)
}

func optionalUnorderedDomainSliceEqual[T any](prior, current *[]T) bool {
	if prior == nil || current == nil {
		return prior == nil && current == nil
	}
	return unorderedDomainSliceEqual(*prior, *current)
}

func unorderedDomainSliceEqual[T any](prior, current []T) bool {
	if len(prior) != len(current) {
		return false
	}
	matched := make([]bool, len(current))
	for _, priorValue := range prior {
		found := false
		for index, currentValue := range current {
			if matched[index] || !reflect.DeepEqual(priorValue, currentValue) {
				continue
			}
			matched[index] = true
			found = true
			break
		}
		if !found {
			return false
		}
	}
	return true
}

func domainSliceHasDuplicate[T any](values []T) bool {
	for index, value := range values {
		for priorIndex := 0; priorIndex < index; priorIndex++ {
			if reflect.DeepEqual(values[priorIndex], value) {
				return true
			}
		}
	}
	return false
}
