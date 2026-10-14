package integerprecision

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/rudderlabs/rudder-go-kit/jsonrs"
)

const ConfigKey = "Processor.preserveIntegerPrecision"

type Number string

var numberPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

func (n Number) String() string { return string(n) }

func (n Number) Float64() (float64, error) { return strconv.ParseFloat(string(n), 64) }

func (n Number) MarshalJSON() ([]byte, error) {
	if !numberPattern.MatchString(string(n)) {
		return nil, fmt.Errorf("invalid JSON number %q", n)
	}
	return []byte(n), nil
}

// jsonrs' streaming decoder returns encoding/json.Number, but Sonnet only
// recognises its own Number type. A Marshaler keeps the token numeric for every library.
func preserveNumbers(value reflect.Value) {
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return
		}
		if number, ok := value.Interface().(json.Number); ok && value.CanSet() {
			value.Set(reflect.ValueOf(Number(number)))
			return
		}
		preserveNumbers(value.Elem())
	case reflect.Pointer:
		if !value.IsNil() {
			preserveNumbers(value.Elem())
		}
	case reflect.Struct:
		for _, field := range value.Fields() {
			if field.CanSet() {
				preserveNumbers(field)
			}
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			element := reflect.New(value.Type().Elem()).Elem()
			element.Set(value.MapIndex(key))
			preserveNumbers(element)
			value.SetMapIndex(key, element)
		}
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return
		}
		for index := 0; index < value.Len(); index++ {
			preserveNumbers(value.Index(index))
		}
	}
}

func Unmarshal(data []byte, value any, enabled bool) error {
	if !enabled {
		return jsonrs.Unmarshal(data, value)
	}
	decoder := jsonrs.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	preserveNumbers(reflect.ValueOf(value))
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected trailing JSON value")
	}
	return nil
}

// Decimal and exponent notation are evaluated as decimal digits, never through
// float64. The scale is bounded before allocating zeroes, including hostile exponents.
func integerDigits(number json.Number) (string, bool) {
	text := number.String()
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	exponent := 0
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		parsed, err := strconv.ParseInt(text[index+1:], 10, 32)
		if err != nil {
			if strings.HasPrefix(text[index+1:], "-") {
				exponent = -1 << 30
			} else {
				exponent = 1 << 30
			}
		} else {
			exponent = int(parsed)
		}
		text = text[:index]
	}
	if index := strings.IndexByte(text, '.'); index >= 0 {
		exponent -= len(text) - index - 1
		text = text[:index] + text[index+1:]
	}
	text = strings.TrimLeft(text, "0")
	if text == "" {
		return "0", true
	}
	if exponent < 0 {
		if -exponent >= len(text) || strings.Trim(text[len(text)+exponent:], "0") != "" {
			return "", false
		}
		text = text[:len(text)+exponent]
	} else if exponent > 19 || len(text)+exponent > 19 {
		return "", true
	} else {
		text += strings.Repeat("0", exponent)
	}
	if negative {
		text = "-" + text
	}
	return text, true
}

func IsInteger(number json.Number) bool {
	_, whole := integerDigits(number)
	return whole
}

func Int64(value any) (int64, error) {
	switch v := value.(type) {
	case Number:
		return Int64(json.Number(v))
	case json.Number:
		text, whole := integerDigits(v)
		if !whole {
			return 0, fmt.Errorf("number %s is not an integer", v)
		}
		result, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("number %s is outside the int64 range", v)
		}
		return result, nil
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Trunc(v) != v || v < -9223372036854775808.0 || v >= 9223372036854775808.0 {
			return 0, fmt.Errorf("number %v is not a valid int64", v)
		}
		return int64(v), nil
	default:
		return 0, fmt.Errorf("expected integer, got %T", value)
	}
}

func ValidateIntegers(value any) error {
	switch v := value.(type) {
	case Number:
		return ValidateIntegers(json.Number(v))
	case json.Number:
		if IsInteger(v) {
			_, err := Int64(v)
			return err
		}
	case map[string]any:
		for key, child := range v {
			if err := ValidateIntegers(child); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	case []any:
		for index, child := range v {
			if err := ValidateIntegers(child); err != nil {
				return fmt.Errorf("[%d]: %w", index, err)
			}
		}
	}
	return nil
}
