package server

import (
	"github.com/Wei-Shaw/sub2api/internal/custom"
	"github.com/Wei-Shaw/sub2api/internal/customapp"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ProvideCustomHost is the single application composition boundary. Wire shares
// this exact host between routing, startup and cleanup. Optional providers are
// snapshotted at bootstrap; assembly never mutates the process-wide default.
func ProvideCustomHost(h *handler.Handlers, promptRules *service.PromptRuleService) (*custom.Host, error) {
	return customapp.NewHost(h, promptRules, custom.DefaultHost().Providers())
}
