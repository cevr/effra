package effra

import "encoding/json"

func InspectScope() Effect[string] {
	return func(fc *FiberContext) Exit[string] {
		data, err := json.Marshal(fc.Scope().Snapshot())
		if err != nil {
			return Die[string](err)
		}
		return Succeed(string(data))
	}
}
