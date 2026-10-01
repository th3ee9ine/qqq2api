package service

import "strings"

// isImageGenerationModel recognizes dedicated Gemini image models that may be
// exposed by an OpenAI-compatible upstream, regardless of its account platform.
func isImageGenerationModel(model string) bool {
	modelLower := strings.ToLower(model)
	modelLower = strings.TrimPrefix(modelLower, "models/")
	return modelLower == "gemini-3.1-flash-image" ||
		modelLower == "gemini-3.1-flash-image-preview" ||
		strings.HasPrefix(modelLower, "gemini-3.1-flash-image-") ||
		modelLower == "gemini-3-pro-image" ||
		modelLower == "gemini-3-pro-image-preview" ||
		strings.HasPrefix(modelLower, "gemini-3-pro-image-") ||
		modelLower == "gemini-2.5-flash-image" ||
		modelLower == "gemini-2.5-flash-image-preview" ||
		strings.HasPrefix(modelLower, "gemini-2.5-flash-image-")
}
