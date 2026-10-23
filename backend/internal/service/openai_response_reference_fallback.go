package service

import (
	"errors"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// RemoveOpenAIStoredResponseReferencesFromBody removes opaque server-side
// continuation pointers while retaining the request's available content. It
// is used only when a request must move to a different upstream credential.
func RemoveOpenAIStoredResponseReferencesFromBody(body []byte) ([]byte, error) {
	updated := append([]byte(nil), body...)
	var err error
	for _, field := range []string{"previous_response_id", "conversation"} {
		for actualField := openAITopLevelFieldCaseInsensitive(updated, field); actualField != ""; actualField = openAITopLevelFieldCaseInsensitive(updated, field) {
			previousLength := len(updated)
			updated, err = sjson.DeleteBytes(updated, actualField)
			if err != nil {
				return nil, err
			}
			if len(updated) >= previousLength {
				return nil, errOpenAIStoredResponseReferenceNotRemoved
			}
		}
	}
	inputField := openAITopLevelFieldCaseInsensitive(updated, "input")
	if inputField == "" || !gjson.GetBytes(updated, inputField).IsArray() {
		return updated, nil
	}
	indexes := make([]int, 0)
	gjson.GetBytes(updated, inputField).ForEach(func(index, item gjson.Result) bool {
		if item.IsObject() && strings.EqualFold(openAIResponseInputItemType(item), "item_reference") {
			indexes = append(indexes, int(index.Int()))
		}
		return true
	})
	for i := len(indexes) - 1; i >= 0; i-- {
		updated, err = sjson.DeleteBytes(updated, inputField+"."+strconv.Itoa(indexes[i]))
		if err != nil {
			return nil, err
		}
	}
	return updated, nil
}

var errOpenAIStoredResponseReferenceNotRemoved = errors.New("stored response reference could not be removed")

func openAITopLevelFieldCaseInsensitive(body []byte, target string) string {
	var found string
	gjson.ParseBytes(body).ForEach(func(field, _ gjson.Result) bool {
		if strings.EqualFold(field.String(), target) {
			found = field.String()
			return false
		}
		return true
	})
	return found
}

func openAIResponseInputItemType(item gjson.Result) string {
	var itemType string
	item.ForEach(func(field, value gjson.Result) bool {
		if strings.EqualFold(field.String(), "type") && value.Type == gjson.String {
			itemType = value.String()
			return false
		}
		return true
	})
	return itemType
}
