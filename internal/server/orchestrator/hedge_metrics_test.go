package orchestrator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestComputeHedgeMetrics_BasicTPSCalculation(t *testing.T) {
	windowDuration := 3 * time.Second

	primaryEvents := []*StreamEvent{
		{Data: []byte("token1")},
		{Data: []byte("token2")},
		{Data: []byte("token3")},
		{Data: []byte("token4")},
		{Data: []byte("token5")},
		{Data: []byte("token6")},
	}

	secondaryEvents := []*StreamEvent{
		{Data: []byte("token1")},
		{Data: []byte("token2")},
		{Data: []byte("token3")},
	}

	result := ComputeHedgeMetrics(primaryEvents, secondaryEvents, windowDuration)

	assert.Equal(t, 2.0, result.PrimaryTPS)
	assert.Equal(t, 1.0, result.SecondaryTPS)
	assert.Equal(t, 0, result.WinnerIndex)
	assert.Equal(t, 1, result.LoserIndex)
}

func TestComputeHedgeMetrics_ZeroEvents(t *testing.T) {
	windowDuration := 3 * time.Second

	result := ComputeHedgeMetrics(nil, nil, windowDuration)

	assert.Equal(t, 0.0, result.PrimaryTPS)
	assert.Equal(t, 0.0, result.SecondaryTPS)
	assert.Equal(t, 0, result.WinnerIndex)
}

func TestComputeHedgeMetrics_DoneSentinelExcluded(t *testing.T) {
	windowDuration := 1 * time.Second

	primaryEvents := []*StreamEvent{
		{Data: []byte("token1")},
		{Data: []byte("token2")},
		{Data: []byte("[DONE]")},
		{Data: []byte("token3")},
	}

	result := ComputeHedgeMetrics(primaryEvents, nil, windowDuration)

	assert.Equal(t, 3.0, result.PrimaryTPS)
}

func TestComputeHedgeMetrics_EmptyDataExcluded(t *testing.T) {
	windowDuration := 1 * time.Second

	primaryEvents := []*StreamEvent{
		{Data: []byte("token1")},
		{Data: []byte("")},
		{Data: []byte("token2")},
	}

	result := ComputeHedgeMetrics(primaryEvents, nil, windowDuration)

	assert.Equal(t, 2.0, result.PrimaryTPS)
}

func TestComputeHedgeMetrics_TieBreaking(t *testing.T) {
	windowDuration := 2 * time.Second

	primaryEvents := []*StreamEvent{
		{Data: []byte("token1")},
		{Data: []byte("token2")},
	}

	secondaryEvents := []*StreamEvent{
		{Data: []byte("token1")},
		{Data: []byte("token2")},
	}

	result := ComputeHedgeMetrics(primaryEvents, secondaryEvents, windowDuration)

	assert.Equal(t, 1.0, result.PrimaryTPS)
	assert.Equal(t, 1.0, result.SecondaryTPS)
	assert.Equal(t, 0, result.WinnerIndex)
}

func TestComputeHedgeMetrics_SecondaryWins(t *testing.T) {
	windowDuration := 2 * time.Second

	primaryEvents := []*StreamEvent{
		{Data: []byte("token1")},
	}

	secondaryEvents := []*StreamEvent{
		{Data: []byte("token1")},
		{Data: []byte("token2")},
		{Data: []byte("token3")},
		{Data: []byte("token4")},
	}

	result := ComputeHedgeMetrics(primaryEvents, secondaryEvents, windowDuration)

	assert.Equal(t, 0.5, result.PrimaryTPS)
	assert.Equal(t, 2.0, result.SecondaryTPS)
	assert.Equal(t, 1, result.WinnerIndex)
	assert.Equal(t, 0, result.LoserIndex)
}

func TestComputeHedgeMetrics_WindowDuration(t *testing.T) {
	windowDuration := 5 * time.Second

	primaryEvents := []*StreamEvent{
		{Data: []byte("token1")},
		{Data: []byte("token2")},
		{Data: []byte("token3")},
		{Data: []byte("token4")},
		{Data: []byte("token5")},
	}

	result := ComputeHedgeMetrics(primaryEvents, nil, windowDuration)

	assert.Equal(t, 1.0, result.PrimaryTPS)
	assert.Equal(t, 5*time.Second, result.ObservationWindowDuration)
}

func TestComputeHedgeMetrics_ZeroWindowDuration(t *testing.T) {
	primaryEvents := []*StreamEvent{
		{Data: []byte("token1")},
	}

	result := ComputeHedgeMetrics(primaryEvents, nil, 0)

	assert.Equal(t, 0.0, result.PrimaryTPS)
}

func TestComputeObservationTPS_AllSentinels(t *testing.T) {
	windowDuration := 1 * time.Second

	events := []*StreamEvent{
		{Data: []byte("[DONE]")},
		{Data: []byte("[DONE]")},
		{Data: []byte("[DONE]")},
	}

	result := computeObservationTPS(events, windowDuration)

	assert.Equal(t, 0.0, result)
}

func TestComputeObservationTPS_MixedEvents(t *testing.T) {
	windowDuration := 2 * time.Second

	events := []*StreamEvent{
		{Data: []byte("")},
		{Data: []byte("real1")},
		{Data: []byte("[DONE]")},
		{Data: []byte("")},
		{Data: []byte("real2")},
		{Data: []byte("real3")},
	}

	result := computeObservationTPS(events, windowDuration)

	assert.Equal(t, 1.5, result)
}

func TestComputeHedgeMetrics_TypeDoneExcluded(t *testing.T) {
	windowDuration := 1 * time.Second

	primaryEvents := []*StreamEvent{
		{Data: []byte("token1")},
		{Data: []byte("token2")},
		{Type: "done", Data: []byte("[DONE]")},
		{Data: []byte("token3")},
	}

	result := ComputeHedgeMetrics(primaryEvents, nil, windowDuration)

	assert.Equal(t, 3.0, result.PrimaryTPS)
}

func TestComputeHedgeMetrics_TypeDoneEventWithDifferentData(t *testing.T) {
	windowDuration := 1 * time.Second

	primaryEvents := []*StreamEvent{
		{Data: []byte("token1")},
		{Type: "done", Data: []byte("some_data")},
		{Data: []byte("token2")},
	}

	result := ComputeHedgeMetrics(primaryEvents, nil, windowDuration)

	assert.Equal(t, 2.0, result.PrimaryTPS)
}