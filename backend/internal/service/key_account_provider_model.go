package service

import "github.com/tidwall/gjson"

// keyAccountInferenceJSONModel reads the already-built upstream envelope. In
// particular, Antigravity's requestedModel and error metadata may name a public
// alias rather than the actual model sent by native retries and fallback paths.
// Missing/non-string fields stay unknown; this observer never guesses a model or
// changes the native request body.
func keyAccountInferenceJSONModel(body []byte) string {
	model := gjson.GetBytes(body, "model")
	if model.Type != gjson.String {
		return ""
	}
	return model.String()
}
