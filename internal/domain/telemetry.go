package domain

import "time"

type TelemetryState struct {
	State      string     `json:"state"`
	ObservedAt *time.Time `json:"observed_at"`
	Reason     string     `json:"reason"`
}

type TelemetryValue struct {
	Name       string     `json:"name"`
	Unit       string     `json:"unit"`
	State      string     `json:"state"`
	Value      *float64   `json:"value"`
	ObservedAt *time.Time `json:"observed_at"`
	Reason     string     `json:"reason"`
}

type TelemetryOperations struct {
	Queued           int `json:"queued"`
	Running          int `json:"running"`
	RecoveryRequired int `json:"recovery_required"`
}

type TelemetrySummary struct {
	ObservedAt               time.Time           `json:"observed_at"`
	Mode                     string              `json:"mode"`
	MutationStorageAvailable bool                `json:"mutation_storage_available"`
	Operations               TelemetryOperations `json:"operations"`
	MetricsExport            TelemetryState      `json:"metrics_export"`
	TracesExport             TelemetryState      `json:"traces_export"`
	Backend                  TelemetryState      `json:"backend"`
	Values                   []TelemetryValue    `json:"values"`
}
