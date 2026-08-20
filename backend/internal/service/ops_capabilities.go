package service

import "context"

// OpsCapabilities is intentionally narrower than the system settings payload.
// It contains only the feature state required to render the operations page.
type OpsCapabilities struct {
	MonitoringEnabled         bool         `json:"ops_monitoring_enabled"`
	RealtimeMonitoringEnabled bool         `json:"ops_realtime_monitoring_enabled"`
	QueryModeDefault          OpsQueryMode `json:"ops_query_mode_default"`
}

func (s *OpsService) GetCapabilities(ctx context.Context) OpsCapabilities {
	return OpsCapabilities{
		MonitoringEnabled:         s.IsMonitoringEnabled(ctx),
		RealtimeMonitoringEnabled: s.IsRealtimeMonitoringEnabled(ctx),
		QueryModeDefault:          s.resolveOpsQueryMode(ctx, ""),
	}
}
