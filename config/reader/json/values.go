package json

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	simple "github.com/bitly/go-simplejson"
	"github.com/go-admin-team/go-admin-core/config/reader"
	"github.com/go-admin-team/go-admin-core/config/source"
)

type jsonValues struct {
	ch *source.ChangeSet
	sj *simple.Json
}

type jsonValue struct {
	*simple.Json
}

func newValues(ch *source.ChangeSet) (reader.Values, error) {
	sj := simple.New()
	data, _ := reader.ReplaceEnvVars(ch.Data)
	if err := sj.UnmarshalJSON(data); err != nil {
		sj.SetPath(nil, string(ch.Data))
	}
	return &jsonValues{ch, sj}, nil
}

func (j *jsonValues) Get(path ...string) reader.Value {
	return &jsonValue{j.sj.GetPath(path...)}
}

func (j *jsonValues) Del(path ...string) {
	// delete the tree?
	if len(path) == 0 {
		j.sj = simple.New()
		return
	}

	if len(path) == 1 {
		j.sj.Del(path[0])
		return
	}

	vals := j.sj.GetPath(path[:len(path)-1]...)
	vals.Del(path[len(path)-1])
	j.sj.SetPath(path[:len(path)-1], vals.Interface())
	return
}

func (j *jsonValues) Set(val interface{}, path ...string) {
	j.sj.SetPath(path, val)
}

func (j *jsonValues) Bytes() []byte {
	b, _ := j.sj.MarshalJSON()
	return b
}

func (j *jsonValues) Map() map[string]interface{} {
	m, _ := j.sj.Map()
	return m
}

func (j *jsonValues) Scan(v interface{}) error {
	// 🔥 关键调试：记录扫描前状态
	fmt.Printf("\n=== [jsonValues.Scan] 开始扫描 ===\n")
	fmt.Printf("目标对象类型: %T\n", v)

	b, err := j.sj.MarshalJSON()
	if err != nil {
		fmt.Printf("⚠️ [jsonValues.Scan] MarshalJSON 失败: %v\n", err)
		return err
	}

	// 🔥 关键调试：输出生成的 JSON
	fmt.Printf("生成的 JSON 长度: %d bytes\n", len(b))
	jsonStr := ""
	if len(b) > 0 {
		if len(b) < 2000 {
			jsonStr = string(b)
			fmt.Printf("完整的 JSON: %s\n", jsonStr)
		} else {
			jsonStr = string(b)
			fmt.Printf("JSON 前2000字符: %s...\n", string(b[:2000]))
		}

		// 检查 JSON 中是否包含 logger
		if strings.Contains(jsonStr, `"logger"`) {
			fmt.Printf("✓ JSON 包含 'logger' 字段\n")
			// 提取 logger 部分
			if startIdx := strings.Index(jsonStr, `"logger"`); startIdx >= 0 {
				endIdx := startIdx + 300
				if endIdx > len(jsonStr) {
					endIdx = len(jsonStr)
				}
				fmt.Printf("logger 部分: %s\n", jsonStr[startIdx:endIdx])
			}
		} else {
			fmt.Printf("✗ JSON 不包含 'logger' 字段！\n")
			fmt.Printf("检查是否包含 'settings': %v\n", strings.Contains(jsonStr, `"settings"`))
		}
	} else {
		fmt.Printf("⚠️ 生成的 JSON 为空！\n")
	}

	err = json.Unmarshal(b, v)
	if err != nil {
		fmt.Printf("✗ [jsonValues.Scan] json.Unmarshal 失败: %v\n", err)
		fmt.Printf("   JSON 长度: %d bytes\n", len(b))
		if len(b) < 500 {
			fmt.Printf("   完整 JSON: %q\n", string(b))
		}
	} else {
		fmt.Printf("✓ [jsonValues.Scan] json.Unmarshal 成功\n")
		// 尝试反射检查解析后的值
		fmt.Printf("   目标对象地址: %p\n", v)
	}
	fmt.Printf("=== [jsonValues.Scan] 扫描完成 ===\n\n")
	return err
}

func (j *jsonValues) String() string {
	return "json"
}

func (j *jsonValue) Bool(def bool) bool {
	b, err := j.Json.Bool()
	if err == nil {
		return b
	}

	str, ok := j.Interface().(string)
	if !ok {
		return def
	}

	b, err = strconv.ParseBool(str)
	if err != nil {
		return def
	}

	return b
}

func (j *jsonValue) Int(def int) int {
	i, err := j.Json.Int()
	if err == nil {
		return i
	}

	str, ok := j.Interface().(string)
	if !ok {
		return def
	}

	i, err = strconv.Atoi(str)
	if err != nil {
		return def
	}

	return i
}

func (j *jsonValue) String(def string) string {
	return j.Json.MustString(def)
}

func (j *jsonValue) Float64(def float64) float64 {
	f, err := j.Json.Float64()
	if err == nil {
		return f
	}

	str, ok := j.Interface().(string)
	if !ok {
		return def
	}

	f, err = strconv.ParseFloat(str, 64)
	if err != nil {
		return def
	}

	return f
}

func (j *jsonValue) Duration(def time.Duration) time.Duration {
	v, err := j.Json.String()
	if err != nil {
		return def
	}

	value, err := time.ParseDuration(v)
	if err != nil {
		return def
	}

	return value
}

func (j *jsonValue) StringSlice(def []string) []string {
	v, err := j.Json.String()
	if err == nil {
		sl := strings.Split(v, ",")
		if len(sl) > 1 {
			return sl
		}
	}
	return j.Json.MustStringArray(def)
}

func (j *jsonValue) StringMap(def map[string]string) map[string]string {
	m, err := j.Json.Map()
	if err != nil {
		return def
	}

	res := map[string]string{}

	for k, v := range m {
		res[k] = fmt.Sprintf("%v", v)
	}

	return res
}

func (j *jsonValue) Scan(v interface{}) error {
	b, err := j.Json.MarshalJSON()
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (j *jsonValue) Bytes() []byte {
	b, err := j.Json.Bytes()
	if err != nil {
		// try return marshalled
		b, err = j.Json.MarshalJSON()
		if err != nil {
			return []byte{}
		}
		return b
	}
	return b
}
