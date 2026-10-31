package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zyf2007/ChatAPI/internal/http/httpx"
	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/repository/storage"
	outputassetsvc "github.com/zyf2007/ChatAPI/internal/service/chat/outputasset"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

type OutputImageUploader interface {
	UploadOutputImage(context.Context, string, string, string, string, io.Reader) (outputassetsvc.Uploaded, error)
}

type UploadHandler struct {
	Storage             storage.Store
	OutputImages        OutputImageUploader
	OutputImageMaxBytes int64
}

func (h UploadHandler) UploadOutputImage(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := sessionOrLabOwnerID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.OutputImages == nil {
		http.Error(w, "output image upload unavailable", http.StatusServiceUnavailable)
		return
	}
	maxBytes := h.OutputImageMaxBytes
	if maxBytes <= 0 {
		maxBytes = 10 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+(1<<20))
	file, header, err := r.FormFile("image")
	if err != nil {
		status := http.StatusBadRequest
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, "image is required", status)
		return
	}
	defer file.Close()
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	result, err := h.OutputImages.UploadOutputImage(r.Context(), ownerID, conversationID, header.Filename, header.Header.Get("Content-Type"), file)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, outputassetsvc.ErrAssetNotFound) || errors.Is(err, turnsvc.ErrPendingNotFound) {
			status = http.StatusNotFound
		} else if !errors.Is(err, media.ErrInvalidImageInput) &&
			!errors.Is(err, media.ErrImageTooLarge) &&
			!errors.Is(err, turnsvc.ErrOutputImageNotAllowed) {
			status = http.StatusInternalServerError
		}
		httpx.WriteJSON(w, status, map[string]any{"error": map[string]any{"message": err.Error()}})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, result)
}

func (h UploadHandler) GetImage(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := sessionOrLabOwnerID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	fileID := sanitizePathSegment(chi.URLParam(r, "fileID"))
	if fileID == "" {
		http.NotFound(w, r)
		return
	}
	asset, err := h.Storage.GetMediaAssetByFileID(r.Context(), fileID)
	if err != nil || strings.TrimSpace(asset.OwnerID) != ownerID {
		http.NotFound(w, r)
		return
	}
	if _, err := os.Stat(asset.Path); err != nil {
		if os.IsNotExist(err) {
			applyUploadSecurityHeaders(w)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte("image unavailable"))
			return
		}
		http.Error(w, "failed to read image", http.StatusInternalServerError)
		return
	}
	applyUploadSecurityHeaders(w)
	w.Header().Set("Content-Type", asset.MediaType)
	w.Header().Set("Content-Disposition", "inline; filename=\""+filepath.Base(fileID)+".avif\"")
	http.ServeFile(w, r, asset.Path)
}

func sanitizePathSegment(value string) string {
	value = filepath.Base(strings.TrimSpace(value))
	if value == "." || value == "/" || value == "" {
		return ""
	}
	return value
}

func applyUploadSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; sandbox")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
}
