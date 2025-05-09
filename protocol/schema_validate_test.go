package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

func Test_Validate(t *testing.T) {
	type args struct {
		data   any
		schema Property
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		// string integer number boolean
		{"", args{data: "ABC", schema: Property{Type: String}}, true},
		{"", args{data: 123, schema: Property{Type: String}}, false},
		{"", args{data: "a", schema: Property{Type: String, Enum: []string{"a", "b", "c"}}}, true},
		{"", args{data: "d", schema: Property{Type: String, Enum: []string{"a", "b", "c"}}}, false},
		{"", args{data: 123, schema: Property{Type: Integer}}, true},
		{"", args{data: 123.4, schema: Property{Type: Integer}}, false},
		{"", args{data: 1, schema: Property{Type: Integer, Enum: []string{"1", "2", "3"}}}, true},
		{"", args{data: 4, schema: Property{Type: Integer, Enum: []string{"1", "2", "3"}}}, false},
		{"", args{data: "ABC", schema: Property{Type: Number}}, false},
		{"", args{data: 123, schema: Property{Type: Number}}, true},
		{"", args{data: 1.1, schema: Property{Type: Number, Enum: []string{"1.1", "2.2", "3.3"}}}, true},
		{"", args{data: 4.4, schema: Property{Type: Number, Enum: []string{"1.1", "2.2", "3.3"}}}, false},
		{"", args{data: 1, schema: Property{Type: Number, Enum: []string{"1", "2", "3"}}}, true},
		{"", args{data: 4, schema: Property{Type: Number, Enum: []string{"1", "2", "3"}}}, false},
		{"", args{data: false, schema: Property{Type: Boolean}}, true},
		{"", args{data: 123, schema: Property{Type: Boolean}}, false},
		{"", args{data: nil, schema: Property{Type: Null}}, true},
		{"", args{data: 0, schema: Property{Type: Null}}, false},
		// array
		{"", args{
			data: []any{"a", "b", "c"}, schema: Property{
				Type: Array, Items: &Property{Type: String},
			},
		}, true},
		{"", args{
			data: []any{1, 2, 3}, schema: Property{
				Type: Array, Items: &Property{Type: String},
			},
		}, false},
		{"", args{
			data: []any{"a"}, schema: Property{
				Type: Array, Items: &Property{Type: String, Enum: []string{"a", "b", "c"}},
			},
		}, true},
		{"", args{
			data: []any{"a", "b", "c"}, schema: Property{
				Type: Array, Items: &Property{Type: String, Enum: []string{"a", "b", "c"}},
			},
		}, true},
		{"", args{
			data: []any{"d"}, schema: Property{
				Type: Array, Items: &Property{Type: String, Enum: []string{"a", "b", "c"}},
			},
		}, false},
		{"", args{
			data: []any{"a", "b", "c", "d"}, schema: Property{
				Type: Array, Items: &Property{Type: String, Enum: []string{"a", "b", "c"}},
			},
		}, false},
		{"", args{
			data: []any{1, 2, 3}, schema: Property{
				Type: Array, Items: &Property{Type: Integer},
			},
		}, true},
		{"", args{
			data: []any{1, 2, 3.4}, schema: Property{
				Type: Array, Items: &Property{Type: Integer},
			},
		}, false},
		{"", args{
			data: []any{1}, schema: Property{
				Type: Array, Items: &Property{Type: Integer, Enum: []string{"1", "2", "3"}},
			},
		}, true},
		{"", args{
			data: []any{1, 2, 3}, schema: Property{
				Type: Array, Items: &Property{Type: Integer, Enum: []string{"1", "2", "3"}},
			},
		}, true},
		{"", args{
			data: []any{1, 2, 3, 4}, schema: Property{
				Type: Array, Items: &Property{Type: Integer, Enum: []string{"1", "2", "3"}},
			},
		}, false},
		{"", args{
			data: []any{4}, schema: Property{
				Type: Array, Items: &Property{Type: Integer, Enum: []string{"1", "2", "3"}},
			},
		}, false},
		// object
		{"", args{data: map[string]any{
			"string":  "abc",
			"integer": 123,
			"number":  123.4,
			"boolean": false,
			"array":   []any{1, 2, 3},
		}, schema: Property{
			Type: ObjectT, Properties: map[string]*Property{
				"string":  {Type: String},
				"integer": {Type: Integer},
				"number":  {Type: Number},
				"boolean": {Type: Boolean},
				"array":   {Type: Array, Items: &Property{Type: Number}},
			},
			Required: []string{"string"},
		}}, true},
		{"", args{data: map[string]any{
			"integer": 123,
			"number":  123.4,
			"boolean": false,
			"array":   []any{1, 2, 3},
		}, schema: Property{
			Type: ObjectT, Properties: map[string]*Property{
				"string":  {Type: String},
				"integer": {Type: Integer},
				"number":  {Type: Number},
				"boolean": {Type: Boolean},
				"array":   {Type: Array, Items: &Property{Type: Number}},
			},
			Required: []string{"string"},
		}}, false},
		{"", args{data: map[string]any{
			"string":     "a",
			"integer":    1,
			"number":     1.1,
			"number4Int": 1,
			"array":      []any{1, 2, 3},
		}, schema: Property{
			Type: ObjectT, Properties: map[string]*Property{
				"string":     {Type: String, Enum: []string{"a", "b", "c"}},
				"integer":    {Type: Integer, Enum: []string{"1", "2", "3"}},
				"number":     {Type: Number, Enum: []string{"1.1", "2.2", "3.3"}},
				"number4Int": {Type: Number, Enum: []string{"1", "2", "3"}},
				"array":      {Type: Array, Items: &Property{Type: Number}, Enum: []string{"1", "2", "3"}},
			},
			Required: []string{"string"},
		}}, true},
		{"", args{data: map[string]any{
			"string":     "d",
			"integer":    4,
			"number":     4.4,
			"number4Int": 4,
			"array":      []any{4},
		}, schema: Property{
			Type: ObjectT, Properties: map[string]*Property{
				"string":     {Type: String, Enum: []string{"a", "b", "c"}},
				"integer":    {Type: Integer, Enum: []string{"1", "2", "3"}},
				"number":     {Type: Number, Enum: []string{"1.1", "2.2", "3.3"}},
				"number4Int": {Type: Number, Enum: []string{"1", "2", "3"}},
				"array":      {Type: Array, Items: &Property{Type: Number}, Enum: []string{"1", "2", "3"}},
			},
			Required: []string{"string"},
		}}, false},
		{"nested anonymous struct", args{data: map[string]any{
			"user": map[string]any{
				"name": "test",
				"info": map[string]any{
					"age":    30,
					"active": true,
				},
			},
		}, schema: Property{
			Type: ObjectT, Properties: map[string]*Property{
				"user": {
					Type: ObjectT,
					Properties: map[string]*Property{
						"name": {Type: String},
						"info": {
							Type: ObjectT,
							Properties: map[string]*Property{
								"age":    {Type: Integer},
								"active": {Type: Boolean},
							},
							Required: []string{"active"},
						},
					},
					Required: []string{"name", "info"},
				},
			},
			Required: []string{"user"},
		}}, true},
		{"nested anonymous struct with invalid data", args{data: map[string]any{
			"user": map[string]any{
				"name": "test",
				"info": map[string]any{
					"age":    "not_a_number",
					"active": true,
				},
			},
		}, schema: Property{
			Type: ObjectT, Properties: map[string]*Property{
				"user": {
					Type: ObjectT,
					Properties: map[string]*Property{
						"name": {Type: String},
						"info": {
							Type: ObjectT,
							Properties: map[string]*Property{
								"age":    {Type: Integer},
								"active": {Type: Boolean},
							},
							Required: []string{"active"},
						},
					},
					Required: []string{"name", "info"},
				},
			},
			Required: []string{"user"},
		}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validate(tt.args.schema, tt.args.data); got != tt.want {
				t.Errorf("validate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnmarshal(t *testing.T) {
	type args struct {
		schema  Property
		content []byte
		v       any
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"", args{
			schema: Property{
				Type: ObjectT,
				Properties: map[string]*Property{
					"string": {Type: String},
					"number": {Type: Number},
				},
			},
			content: []byte(`{"string":"abc","number":123.4}`),
			v: &struct {
				String string  `json:"string"`
				Number float64 `json:"number"`
			}{},
		}, false},
		{"", args{
			schema: Property{
				Type: ObjectT,
				Properties: map[string]*Property{
					"string": {Type: String},
					"number": {Type: Number},
				},
				Required: []string{"string", "number"},
			},
			content: []byte(`{"string":"abc"}`),
			v: struct {
				String string  `json:"string"`
				Number float64 `json:"number"`
			}{},
		}, true},
		{"validate integer", args{
			schema: Property{
				Type: ObjectT,
				Properties: map[string]*Property{
					"string":  {Type: String},
					"integer": {Type: Integer},
				},
				Required: []string{"string", "integer"},
			},
			content: []byte(`{"string":"abc","integer":123}`),
			v: &struct {
				String  string `json:"string"`
				Integer int    `json:"integer"`
			}{},
		}, false},
		{"validate integer failed", args{
			schema: Property{
				Type: ObjectT,
				Properties: map[string]*Property{
					"string":  {Type: String},
					"integer": {Type: Integer},
				},
				Required: []string{"string", "integer"},
			},
			content: []byte(`{"string":"abc","integer":123.4}`),
			v: &struct {
				String  string `json:"string"`
				Integer int    `json:"integer"`
			}{},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifySchemaAndUnmarshal(tt.args.schema, tt.args.content, tt.args.v)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestVerifyAndUnmarshal(t *testing.T) {
	type testData struct {
		String  string  `json:"string"`           // required
		Number  float64 `json:"number,omitempty"` // optional
		Integer int     `json:"-"`                // ignore

		String4Enum  string         `json:"string4enum,omitempty" enum:"a,b,c"`       // enum
		Integer4Enum int            `json:"integer4enum,omitempty" enum:"1,2,3"`      // enum
		Number4Enum  float64        `json:"number4enum,omitempty" enum:"1.1,2.2,3.3"` // enum
		Number4Enum2 int            `json:"number4enum2,omitempty" enum:"1,2,3"`      // enum
		MM           map[string]any `json:"mm,omitempty"`
	}

	// Generate schema for anonymous struct first
	anonymousStructSchema, _ := generateSchemaFromReqStruct(struct {
		Name   string `json:"name"`
		Age    int    `json:"age,omitempty"`
		Active bool   `json:"active"`
	}{})

	// Save the schema in cache for test
	schemaCache.Store(getTypeUUID(reflect.TypeOf(struct {
		Name   string `json:"name"`
		Age    int    `json:"age,omitempty"`
		Active bool   `json:"active"`
	}{})), anonymousStructSchema)

	type args struct {
		content json.RawMessage
		v       any
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "no error",
			args: args{
				content: json.RawMessage("{\"string\":\"abc\",\"number\":123.4}"),
				v:       &testData{},
			},
			wantErr: false,
		},
		{
			name: "string is required",
			args: args{
				content: json.RawMessage("{\"number\":123.4}"),
				v:       &testData{},
			},
			wantErr: true,
		},
		{
			name: "want integer but number",
			args: args{
				content: json.RawMessage("{\"integer\":123.4}"),
				v:       &testData{},
			},
			wantErr: true,
		},
		{
			name: "unmarshal map[string]any",
			args: args{
				content: json.RawMessage("{\"integer\":123.4}"),
				v:       new(map[string]any),
			},
			wantErr: true,
		},
		{
			name: "no error with enum",
			args: args{
				content: json.RawMessage("{\"string\":\"abc\",\"number\":123.4, \"string4enum\":\"a\",\"integer4enum\":1,\"number4enum\":1.1,\"number4enum2\":1}"),
				v:       testData{},
			},
		},
		{
			name: "want a,b,c but d for string4enum",
			args: args{
				content: json.RawMessage("{\"string\":\"abc\",\"number\":123.4, \"string4enum\":\"d\"}"),
				v:       testData{},
			},
			wantErr: true,
		},
		{
			name: "want 1,2,3 but 4 for integer4enum",
			args: args{
				content: json.RawMessage("{\"string\":\"abc\",\"number\":123.4, \"integer4enum\":4}"),
				v:       testData{},
			},
			wantErr: true,
		},
		{
			name: "want 1.1,2.2,3.3 but 4.4 for number4enum",
			args: args{
				content: json.RawMessage("{\"string\":\"abc\",\"number\":123.4, \"number4enum\":4.4}"),
				v:       testData{},
			},
			wantErr: true,
		},
		{
			name: "want 1,2,3 but 4 for number4enum2",
			args: args{
				content: json.RawMessage("{\"string\":\"abc\",\"number\":123.4, \"number4enum2\":4}"),
				v:       testData{},
			},
			wantErr: true,
		},
		{"validate integer failed", args{
			content: []byte(`{"string":"abc","integer":123.4}`),
			v: &struct {
				String  string `json:"string"`
				Integer int    `json:"integer"`
			}{},
		}, true},
		{"validate anonymous struct", args{
			content: []byte(`{"name":"test","active":true}`),
			v: &struct {
				Name   string `json:"name"`
				Age    int    `json:"age,omitempty"`
				Active bool   `json:"active"`
			}{},
		}, false},
		{"validate anonymous struct with invalid data", args{
			content: []byte(`{"active":"not_a_bool"}`),
			v: &struct {
				Name   string `json:"name"`
				Age    int    `json:"age,omitempty"`
				Active bool   `json:"active"`
			}{},
		}, true},
		{"validate anonymous struct missing required field", args{
			content: []byte(`{"age":30,"active":true}`),
			v: &struct {
				Name   string `json:"name"`
				Age    int    `json:"age,omitempty"`
				Active bool   `json:"active"`
			}{},
		}, true},
		{"validate map struct", args{
			content: []byte(`{"string":"str","mm":{"a":1}}`),
			v:       &testData{},
		}, false},
	}
	_, err := generateSchemaFromReqStruct(testData{})
	if err != nil {
		panic(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := VerifyAndUnmarshal(tt.args.content, tt.args.v); (err != nil) != tt.wantErr {
				t.Errorf("VerifyAndUnmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
