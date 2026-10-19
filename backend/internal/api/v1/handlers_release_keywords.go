package apiv1

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/store"
)

func (h *Handlers) ListReleaseKeywords(ctx context.Context, req ListReleaseKeywordsRequestObject) (ListReleaseKeywordsResponseObject, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok || userID == 0 {
		return ListReleaseKeywords401JSONResponse{Code: http.StatusUnauthorized, Message: "authentication required"}, nil
	}
	prefix := strings.TrimSpace(req.Params.Prefix)
	if prefix == "" || utf8.RuneCountInString(prefix) > store.MaxReleaseKeywordLength {
		return ListReleaseKeywords400JSONResponse{Code: http.StatusBadRequest, Message: "prefix must contain 1 to 256 characters"}, nil
	}
	keywords, err := h.store.ListReleaseKeywords(userID, prefix)
	if err != nil {
		return nil, err
	}
	return ListReleaseKeywords200JSONResponse{Keywords: keywords}, nil
}
