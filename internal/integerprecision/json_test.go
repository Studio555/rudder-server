package integerprecision

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rudderlabs/rudder-go-kit/jsonrs"
)

func TestRoundTrip(t *testing.T) {
	for _, library := range []string{jsonrs.StdLib, jsonrs.SonnetLib, jsonrs.JsoniterLib} {
		t.Run(library, func(t *testing.T) {
			previous := jsonrs.Default
			jsonrs.Default = jsonrs.NewWithLibrary(library)
			t.Cleanup(func() { jsonrs.Default = previous })
			payload := []byte(`{"properties":{"id":9223372036854775807,"nested":[-9223372036854775808,9007199254740993],"decimal":1.25}}`)
			var event map[string]any
			require.NoError(t, Unmarshal(payload, &event, true))
			encoded, err := jsonrs.Marshal(event)
			require.NoError(t, err)
			require.Contains(t, string(encoded), `"id":9223372036854775807`)
			require.Contains(t, string(encoded), `[-9223372036854775808,9007199254740993]`)
			var restored map[string]any
			require.NoError(t, Unmarshal(encoded, &restored, true))
			require.Equal(t, event, restored)
			require.NoError(t, Unmarshal(payload, &event, false))
			require.IsType(t, float64(0), event["properties"].(map[string]any)["id"])
		})
	}
}

func TestInt64(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  int64
	}{
		{"0", 0},
		{"-0", 0},
		{"9007199254740991", 9007199254740991},
		{"9007199254740993", 9007199254740993},
		{"9223372036854775807", 9223372036854775807},
		{"-9223372036854775808", -9223372036854775808},
		{"9007199254740993.0", 9007199254740993},
		{"9.007199254740993e15", 9007199254740993},
		{"100e-2", 1},
		{"0e99999999999", 0},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := Int64(json.Number(tc.input))
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.True(t, IsInteger(json.Number(tc.input)))
		})
	}
	for _, input := range []string{"9223372036854775808", "-9223372036854775809", "1e999999999999", "1.25", "1e-999999999999", "9007199254740993.1"} {
		t.Run(input, func(t *testing.T) {
			_, err := Int64(json.Number(input))
			require.Error(t, err)
		})
	}
}

func TestRejectTrailingJSON(t *testing.T) {
	for _, payload := range []string{`{"id":1} {"id":2}`, `{"id":1} garbage`, `{"id":`} {
		var event map[string]any
		require.Error(t, Unmarshal([]byte(payload), &event, true))
	}
}

func TestValidationReportsNestedPath(t *testing.T) {
	value := map[string]any{"properties": map[string]any{"ids": []any{json.Number("9223372036854775808")}}}
	err := ValidateIntegers(value)
	require.ErrorContains(t, err, "properties: ids: [0]:")
}
