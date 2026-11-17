package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestInstrument(t *testing.T) {
	entity := t.Name() //unique label values, the registry is global

	out, err := Instrument(EntityTypeTrigger, entity, func() ([]byte, error) {
		return []byte("ok"), nil
	})
	assert.Equal(t, []byte("ok"), out)
	assert.NoError(t, err)

	assert.Equal(t, float64(1), testutil.ToFloat64(CommandRuns.WithLabelValues(EntityTypeTrigger, entity)))
	assert.Equal(t, float64(0), testutil.ToFloat64(CommandErrors.WithLabelValues(EntityTypeTrigger, entity)))

	_, _ = Instrument(EntityTypeSensor, entity, func() ([]byte, error) {
		return nil, assert.AnError
	})
	assert.Equal(t, float64(1), testutil.ToFloat64(CommandErrors.WithLabelValues(EntityTypeSensor, entity)))
	assert.Equal(t, float64(1), testutil.ToFloat64(CommandRuns.WithLabelValues(EntityTypeSensor, entity)))
}
