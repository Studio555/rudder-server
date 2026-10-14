package warehouse

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rudderlabs/rudder-go-kit/config"
	"github.com/rudderlabs/rudder-go-kit/jsonrs"
	"github.com/rudderlabs/rudder-go-kit/logger"
	"github.com/rudderlabs/rudder-go-kit/stats"

	backendconfig "github.com/rudderlabs/rudder-server/backend-config"
	"github.com/rudderlabs/rudder-server/internal/integerprecision"
	"github.com/rudderlabs/rudder-server/processor/types"
)

func TestIntegerPrecisionWarehouse(t *testing.T) {
	c := config.New()
	c.Set(integerprecision.ConfigKey, true)
	transformer := New(c, logger.NOP, stats.NOP)
	var message map[string]any
	require.NoError(t, integerprecision.Unmarshal([]byte(`{
		"type":"track","event":"PrecisionCanary","messageId":"precision","userId":"canary-user",
		"timestamp":"2026-10-14T00:00:00.000Z","context":{},
		"properties":{"PlayerId":9223372036854775807,"negative":-9223372036854775808,"exponent":9.007199254740993e15,"fraction":1.25}
	}`), &message, true))
	event := types.TransformerEvent{
		Message:     message,
		Metadata:    types.Metadata{EventType: "track", DestinationType: "BQ", ReceivedAt: "2026-10-14T00:00:00.000Z", MessageID: "precision"},
		Destination: backendconfig.DestinationT{ID: "precision", DestinationDefinition: backendconfig.DestinationDefinitionT{Name: "BQ"}},
	}
	result := transformer.Transform(context.Background(), []types.TransformerEvent{event})
	require.Empty(t, result.FailedEvents)
	require.NotEmpty(t, result.Events)
	encoded, err := jsonrs.Marshal(result.Events)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"player_id":9223372036854775807`)
	require.Contains(t, string(encoded), `"player_id":"int"`)
	require.Contains(t, string(encoded), `"negative":-9223372036854775808`)
	require.Contains(t, string(encoded), `"exponent":9.007199254740993e15`)
	require.Contains(t, string(encoded), `"fraction":"float"`)

	invalid := event
	invalid.Message = map[string]any{"properties": map[string]any{"id": integerprecision.Number("9223372036854775808")}}
	result = transformer.Transform(context.Background(), []types.TransformerEvent{event, invalid})
	require.Len(t, result.FailedEvents, 1)
	require.Equal(t, 400, result.FailedEvents[0].StatusCode)
	require.Contains(t, result.FailedEvents[0].Error, "outside the int64 range")
	require.NotEmpty(t, result.Events)
}
