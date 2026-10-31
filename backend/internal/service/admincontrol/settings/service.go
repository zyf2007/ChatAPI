package settings

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/service/settingscore"
)

type SettingsDomain interface {
	Domain() string
	Title() string
	Fields() []settingscore.Descriptor
	Get(context.Context) (settingscore.Document, error)
	Reload(context.Context) (settingscore.Document, error)
	ValidatePatch(context.Context, map[string]any) error
	Patch(context.Context, map[string]any) (settingscore.Document, []string, error)
}
type Domain struct {
	Settings    SettingsDomain
	AfterUpdate func(context.Context, []string) error
}
type Service struct {
	domains map[string]Domain
	runtime config.Config
}
type PatchInput struct {
	Values map[string]any `json:"values"`
}
type PatchResult struct {
	Document        settingscore.Document `json:"document"`
	Applied         []string              `json:"applied"`
	RestartRequired []string              `json:"restart_required"`
	Warnings        []string              `json:"warnings,omitempty"`
}

func New(runtime config.Config, domains ...Domain) *Service {
	items := map[string]Domain{}
	for _, d := range domains {
		if d.Settings != nil && (runtime.Mode != config.ModeLab || d.Settings.Domain() == "access" || d.Settings.Domain() == "media") {
			items[d.Settings.Domain()] = d
		}
	}
	return &Service{domains: items, runtime: runtime}
}
func (s *Service) Get(ctx context.Context, domain string) (settingscore.Document, error) {
	d, ok := s.domains[strings.TrimSpace(domain)]
	if !ok {
		return settingscore.Document{}, common.ErrNotFound
	}
	return d.Settings.Get(ctx)
}
func (s *Service) Patch(ctx context.Context, domain string, input PatchInput) (PatchResult, error) {
	d, ok := s.domains[strings.TrimSpace(domain)]
	if !ok {
		return PatchResult{}, common.ErrNotFound
	}
	if len(input.Values) == 0 {
		return PatchResult{}, fmt.Errorf("settings patch must not be empty")
	}
	doc, restart, err := d.Settings.Patch(ctx, input.Values)
	if err != nil {
		return PatchResult{}, err
	}
	applied := make([]string, 0, len(input.Values))
	for k := range input.Values {
		applied = append(applied, k)
	}
	sort.Strings(applied)
	sort.Strings(restart)
	result := PatchResult{Document: doc, Applied: applied, RestartRequired: restart}
	if d.AfterUpdate != nil {
		if err := d.AfterUpdate(ctx, applied); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("settings were saved but reconciliation failed: %v", err))
		}
	}
	return result, nil
}
func (s *Service) Catalog() map[string]any {
	groups := make([]map[string]any, 0, len(s.domains))
	keys := make([]string, 0, len(s.domains))
	for k := range s.domains {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		d := s.domains[k]
		groups = append(groups, map[string]any{"domain": k, "title": d.Settings.Title(), "fields": d.Settings.Fields()})
	}
	return map[string]any{"version": 1, "groups": groups}
}
func (s *Service) Overview(ctx context.Context) (map[string]any, error) {
	docs := map[string]settingscore.Document{}
	warnings := []string{}
	for key, d := range s.domains {
		doc, err := d.Settings.Get(ctx)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", key, err))
			continue
		}
		if doc.Stale {
			warnings = append(warnings, fmt.Sprintf("%s: using stale settings snapshot: %s", key, doc.RefreshError))
		}
		docs[key] = doc
	}
	return map[string]any{"domains": docs, "warnings": warnings, "runtime": map[string]any{"mode": s.runtime.Mode, "database_driver": s.runtime.DatabaseDriver, "smtp_configured": s.runtime.SMTPEnabled, "oidc_configured": s.runtime.OIDCEnabled}}, nil
}
func (s *Service) Runtime() map[string]any {
	return map[string]any{"source": "environment", "editable": false, "restart_required": true, "config": s.runtime.Redacted()}
}
