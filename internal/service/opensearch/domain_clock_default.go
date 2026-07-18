package opensearch

func newDomainClock() domainClock {
	return realDomainClock{}
}
