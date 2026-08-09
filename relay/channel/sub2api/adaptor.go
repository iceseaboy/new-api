package sub2api

import (
	"github.com/QuantumNous/opclink/relay/channel/opclink"
)

type Adaptor struct {
	opclink.Adaptor
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
