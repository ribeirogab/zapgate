package webhooks

import "encoding/json"

const maxBodyBytes = 60_000

func withinCap(body Body, data receivedData) (Body, error) {
	body.Data = data
	size, err := encodedSize(body)
	if err != nil || size <= maxBodyBytes {
		return body, err
	}
	messages := data.Messages
	data.Messages, data.Truncated = []messageView{}, true
	body.Data = data
	frame, err := encodedSize(body)
	if err != nil {
		return Body{}, err
	}
	kept, err := newestThatFit(messages, maxBodyBytes-frame)
	data.Messages = messages[len(messages)-kept:]
	body.Data = data
	return body, err
}

func newestThatFit(messages []messageView, room int) (int, error) {
	kept := 0
	for i := len(messages) - 1; i >= 0; i-- {
		size, err := encodedSize(messages[i])
		if err != nil {
			return 0, err
		}
		if kept > 0 {
			size += len(",")
		}
		if size > room {
			break
		}
		room -= size
		kept++
	}
	return kept, nil
}

func encodedSize(value any) (int, error) {
	encoded, err := json.Marshal(value)
	return len(encoded), err
}
