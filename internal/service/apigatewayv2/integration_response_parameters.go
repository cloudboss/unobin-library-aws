package apigatewayv2

// IntegrationResponseParameter transforms the HTTP response from a backend
// integration for one status code before it returns to clients, supported
// only for HTTP APIs. The status code selects which responses the mappings
// apply to and must be within 200-599, a range the API enforces. Each mapping
// key follows <action>:<header>.<location> or overwrite.statuscode, where the
// action is append, overwrite, or remove; values may be static or map
// response data, stage variables, or context variables evaluated at runtime.
type IntegrationResponseParameter struct {
	StatusCode string            `ub:"status-code"`
	Mappings   map[string]string `ub:"mappings"`
}

// integrationResponseParameterMap converts the response-parameters list to
// the map of maps the API takes, keyed by status code, for a create. Entries
// with no mappings are skipped, since an empty mapping set declares nothing
// on create; on update the empty set is the per-code delete sentinel, which
// integrationResponseParameterUpdates adds itself. A list with no usable
// entries converts to nil so the member stays out of the request.
func integrationResponseParameterMap(
	params []IntegrationResponseParameter,
) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, p := range params {
		if len(p.Mappings) == 0 {
			continue
		}
		out[p.StatusCode] = p.Mappings
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// integrationResponseParameterUpdates builds the response-parameters member
// of an update: the full desired map, plus an empty mapping set for each
// status code that was configured before and is absent now, which is the
// documented way to delete that code's mappings. Removing the whole list
// maps every prior code to the empty set.
func integrationResponseParameterUpdates(
	prior, desired []IntegrationResponseParameter,
) map[string]map[string]string {
	updates := map[string]map[string]string{}
	for _, p := range desired {
		if len(p.Mappings) == 0 {
			continue
		}
		updates[p.StatusCode] = p.Mappings
	}
	for _, p := range prior {
		if _, ok := updates[p.StatusCode]; !ok {
			updates[p.StatusCode] = map[string]string{}
		}
	}
	return updates
}
