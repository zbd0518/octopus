package relay

import dbmodel "github.com/lingyuins/octopus/internal/model"

func resolveRelayLogEndpointType(requestedEndpointType, matchedGroupEndpointType string) string {
	requested := dbmodel.NormalizeEndpointType(requestedEndpointType)
	trimmed := dbmodel.NormalizeEndpointType(matchedGroupEndpointType)

	// matched 为空（group 未指定）或 '*'（全部）时，用请求的类型记录日志。
	// NormalizeEndpointType 空值回退 chat，需在 normalize 前判断空串。
	if matchedGroupEndpointType == "" || trimmed == dbmodel.EndpointTypeAll {
		return requested
	}

	return trimmed
}
