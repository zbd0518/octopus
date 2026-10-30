package relay

import (
	"context"
	"fmt"
	"strings"

	dbmodel "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/group"
	"github.com/lingyuins/octopus/internal/op/modelmapping"
	"github.com/lingyuins/octopus/internal/utils/log"
)

func stickyRoutingKey(matchedGroup *dbmodel.Group, clientModel string) string {
	if matchedGroup == nil {
		return clientModel
	}
	return fmt.Sprintf("%d:%s:%s:g%d:m%d", matchedGroup.ID, matchedGroup.EndpointType, clientModel, group.Generation(), modelmapping.Generation())
}

type relayModelIdentity struct {
	client   string
	route    string
	override string
	zen      bool
}

func resolveRelayModel(ctx context.Context, endpoint, requested string) (dbmodel.Group, relayModelIdentity, error) {
	identity := relayModelIdentity{client: strings.TrimSpace(requested)}
	identity.route = identity.client
	_, identity.zen = resolveRequestedUpstreamModel(identity.client)
	mapped, matched := identity.client, false
	if !identity.zen {
		mapped, matched = modelmapping.ResolveGlobal(identity.client)
		identity.route = mapped
	}
	matchedGroup, err := group.GroupGetEnabledMapByEndpoint(endpoint, identity.route, ctx)
	if err != nil && matched && mapped != identity.client {
		matchedGroup, err = group.GroupGetEnabledMapByEndpoint(endpoint, identity.client, ctx)
		if err == nil {
			identity.route = identity.client
			identity.override = mapped
			log.Infof("model mapping uses original group fallback: requested=%s target=%s group_id=%d", identity.client, mapped, matchedGroup.ID)
		}
	}
	if err != nil {
		return matchedGroup, identity, err
	}
	if !identity.zen {
		if target, ok := modelmapping.ResolveGroup(identity.client, matchedGroup.ID); ok {
			identity.override = target
		}
	}
	return matchedGroup, identity, nil
}

func (identity relayModelIdentity) candidate(item dbmodel.GroupItem) string {
	if identity.override != "" {
		return identity.override
	}
	if identity.zen {
		if target, ok := resolveRequestedUpstreamModel(identity.client); ok {
			return target
		}
	}
	return strings.TrimSpace(item.ModelName)
}
