package catalog

import (
	"context"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

type ModelKeyStore interface {
	ListModelsByUser(context.Context, string) ([]common.VirtualModel, error)
}

type Service struct {
	modelKeys ModelKeyStore
}

type ModelDescriptor struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func New(modelKeys ModelKeyStore) *Service {
	return &Service{modelKeys: modelKeys}
}

func (s *Service) ListModelsForOwner(ctx context.Context, ownerID string) ([]ModelDescriptor, error) {
	if s == nil || s.modelKeys == nil {
		return nil, nil
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return nil, nil
	}

	items, err := s.modelKeys.ListModelsByUser(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	models := make([]ModelDescriptor, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		models = append(models, ModelDescriptor{
			ID:      name,
			Object:  "model",
			Created: item.CreatedAt.Unix(),
			OwnedBy: "chatapi",
		})
	}
	return models, nil
}
