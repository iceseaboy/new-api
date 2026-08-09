package relay

import (
	relaycommon "github.com/QuantumNous/opclink/relay/common"
	"github.com/QuantumNous/opclink/relaykit/types"
)

func opclinkErrorFromParamOverride(err error) *types.OPCLinkError {
	if fixedErr, ok := relaycommon.AsParamOverrideReturnError(err); ok {
		return relaycommon.OPCLinkErrorFromParamOverride(fixedErr)
	}
	return types.NewError(err, types.ErrorCodeChannelParamOverrideInvalid, types.ErrOptionWithSkipRetry())
}
