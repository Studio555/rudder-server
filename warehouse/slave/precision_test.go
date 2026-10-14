package slave

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rudderlabs/rudder-go-kit/config"
	"github.com/rudderlabs/rudder-go-kit/logger"

	"github.com/rudderlabs/rudder-server/internal/integerprecision"
	"github.com/rudderlabs/rudder-server/warehouse/encoding"
	"github.com/rudderlabs/rudder-server/warehouse/internal/model"
	warehouseutils "github.com/rudderlabs/rudder-server/warehouse/utils"
	"github.com/rudderlabs/rudder-server/warehouse/utils/types"
)

func TestIntegerPrecisionBigQueryLoadFile(t *testing.T) {
	var event types.BatchRouterEvent
	require.NoError(t, integerprecision.Unmarshal([]byte(`{"metadata":{"table":"precision","columns":{"player_id":"int"}},"data":{"player_id":9223372036854775807}}`), &event, true))
	column, ok := event.GetColumnInfo("player_id")
	require.True(t, ok)
	value, err := integerprecision.Int64(column.Value)
	require.NoError(t, err)
	loader := encoding.NewFactory(config.New()).NewEventLoader(nil, warehouseutils.LoadFileTypeJson, warehouseutils.BQ)
	loader.AddColumn("player_id", column.Type, value)
	line, err := loader.WriteToString()
	require.NoError(t, err)
	require.Equal(t, "{\"player_id\":9223372036854775807}\n", line)
	var restored types.BatchRouterEvent
	require.NoError(t, integerprecision.Unmarshal([]byte(`{"data":`+line+`}`), &restored, true))
	require.Equal(t, integerprecision.Number("9223372036854775807"), restored.Data["player_id"])
}

func TestIntegerPrecisionSchemaChanges(t *testing.T) {
	value, err := HandleSchemaChange(logger.NOP, model.IntDataType, model.FloatDataType, integerprecision.Number("9007199254740993.0"))
	require.NoError(t, err)
	require.Equal(t, int64(9007199254740993), value)
	_, err = HandleSchemaChange(logger.NOP, model.IntDataType, model.FloatDataType, integerprecision.Number("9007199254740993.1"))
	require.ErrorContains(t, err, "not an integer")
	_, err = HandleSchemaChange(logger.NOP, model.IntDataType, model.IntDataType, integerprecision.Number("9223372036854775808"))
	require.ErrorContains(t, err, "outside the int64 range")
}
