package helps

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
)

// TranscriptionRequest is an OpenAI multipart request, including its routing fields.
type TranscriptionRequest struct {
	Payload        []byte
	ContentType    string
	Model          string
	ResponseFormat string
}

// PrepareTranscriptionRequest accepts multipart uploads or OpenRouter-style Base64 JSON.
// It preserves multipart file headers and repeated fields and replaces only the model
// when an upstream alias is supplied. JSON audio is converted to an OpenAI file upload.
func PrepareTranscriptionRequest(payload []byte, contentType, upstreamModel string) (TranscriptionRequest, error) {
	var result TranscriptionRequest
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return result, fmt.Errorf("invalid transcription Content-Type")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := make(map[string][]string)
	fileCount := 0
	switch mediaType {
	case "application/json":
		var object map[string]json.RawMessage
		if errUnmarshal := json.Unmarshal(payload, &object); errUnmarshal != nil || object == nil {
			return result, fmt.Errorf("transcription body must be a JSON object")
		}
		var audio struct {
			Data   string `json:"data"`
			Format string `json:"format"`
		}
		if errAudio := json.Unmarshal(object["input_audio"], &audio); errAudio != nil || audio.Data == "" || audio.Format == "" {
			return result, fmt.Errorf("input_audio.data and input_audio.format are required")
		}
		for _, ch := range audio.Format {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')) {
				return result, fmt.Errorf("input_audio.format must be an audio file extension")
			}
		}
		audioBytes, errDecode := base64.StdEncoding.DecodeString(audio.Data)
		if errDecode != nil || len(audioBytes) == 0 {
			return result, fmt.Errorf("input_audio.data must contain valid Base64 audio")
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": "audio." + audio.Format}))
		audioType := mime.TypeByExtension("." + audio.Format)
		if audioType == "" {
			audioType = "application/octet-stream"
		}
		header.Set("Content-Type", audioType)
		part, errPart := writer.CreatePart(header)
		if errPart != nil {
			return result, errPart
		}
		if _, errWrite := part.Write(audioBytes); errWrite != nil {
			return result, errWrite
		}
		fileCount++
		for key, value := range object {
			if key == "input_audio" {
				continue
			}
			if key == "model" || key == "response_format" {
				var str string
				if errValue := json.Unmarshal(value, &str); errValue != nil {
					return result, fmt.Errorf("%s must be a string", key)
				}
			}
			if key == "stream" && string(value) != "false" && string(value) != "null" {
				return result, fmt.Errorf("streaming transcription is not supported")
			}
			if bytes.Equal(value, []byte("null")) {
				continue
			}
			values := []json.RawMessage{value}
			if len(value) > 0 && value[0] == '[' {
				if errValues := json.Unmarshal(value, &values); errValues != nil {
					return result, errValues
				}
				if !strings.HasSuffix(key, "[]") {
					key += "[]"
				}
			}
			for _, item := range values {
				str := string(item)
				if len(item) > 0 && item[0] == '"' {
					if errValue := json.Unmarshal(item, &str); errValue != nil {
						return result, errValue
					}
				}
				fields[key] = append(fields[key], str)
			}
		}
	case "multipart/form-data":
		if params["boundary"] == "" {
			return result, fmt.Errorf("multipart boundary is missing")
		}
		reader := multipart.NewReader(bytes.NewReader(payload), params["boundary"])
		for {
			part, errPart := reader.NextRawPart()
			if errPart == io.EOF {
				break
			}
			if errPart != nil {
				return result, fmt.Errorf("invalid transcription multipart body: %w", errPart)
			}
			name := part.FormName()
			if part.FileName() == "" && name != "file" {
				value, errRead := io.ReadAll(part)
				if errRead != nil {
					return result, errRead
				}
				fields[name] = append(fields[name], string(value))
				continue
			}
			if name == "file" {
				fileCount++
			}
			out, errCreate := writer.CreatePart(part.Header)
			if errCreate != nil {
				return result, errCreate
			}
			n, errCopy := io.Copy(out, part)
			if errCopy != nil {
				return result, errCopy
			}
			if name == "file" && n == 0 {
				return result, fmt.Errorf("audio file must not be empty")
			}
		}
	default:
		return result, fmt.Errorf("transcriptions require application/json or multipart/form-data")
	}
	if fileCount != 1 {
		return result, fmt.Errorf("exactly one audio file is required")
	}
	if len(fields["model"]) != 1 || strings.TrimSpace(fields["model"][0]) == "" {
		return result, fmt.Errorf("model is required and must occur once")
	}
	result.Model = strings.TrimSpace(fields["model"][0])
	if upstreamModel != "" {
		result.Model = upstreamModel
	}
	fields["model"] = []string{result.Model}
	for _, stream := range fields["stream"] {
		if stream != "false" && stream != "0" && stream != "" {
			return result, fmt.Errorf("streaming transcription is not supported")
		}
	}
	if formats := fields["response_format"]; len(formats) > 0 {
		result.ResponseFormat = formats[0]
	}
	for key, values := range fields {
		for _, value := range values {
			if errWrite := writer.WriteField(key, value); errWrite != nil {
				return result, errWrite
			}
		}
	}
	if errClose := writer.Close(); errClose != nil {
		return result, errClose
	}
	result.Payload = body.Bytes()
	result.ContentType = writer.FormDataContentType()
	return result, nil
}
