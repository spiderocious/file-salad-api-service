package uploads

import "github.com/feranmi/file-salad-backend/internal/apperror"

type presignBody struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

func (b presignBody) validate() *apperror.Error {
	fe := map[string][]string{}
	if b.Filename == "" {
		fe["filename"] = []string{"Required"}
	}
	if b.ContentType == "" {
		fe["content_type"] = []string{"Required"}
	}
	if b.Size <= 0 {
		fe["size"] = []string{"Must be greater than zero"}
	}
	if len(fe) > 0 {
		return apperror.Validation("Validation failed", fe)
	}
	return nil
}
