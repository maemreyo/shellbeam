package mcp

func requestOutputDefaults(in input, raw []byte) (int64, int) {
	yieldMS, maxOutput := in.YieldMS, in.MaxOutputBytes
	if !hasField(raw, "yield_time_ms") && in.Action == "start" {
		yieldMS = 10000
	}
	if !hasField(raw, "max_output_bytes") {
		maxOutput = 20000
	}
	return yieldMS, maxOutput
}
