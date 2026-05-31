package main

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const implicitWeixinChannelName = "weixin-direct"

func applyImplicitWeixinChannelDefault(
	root *yaml.Node,
) (bool, error) {
	doc := documentNode(root)
	if doc == nil {
		return false, nil
	}

	channelsNode := mappingValue(doc, channelsKey)
	if channelsNode == nil {
		return false, nil
	}
	if channelsNode.Kind != yaml.SequenceNode {
		return false, fmt.Errorf("config: channels must be a sequence")
	}

	shouldInject, err := shouldInjectImplicitWeixinChannel(
		channelsNode,
	)
	if err != nil {
		return false, err
	}
	if !shouldInject {
		return false, nil
	}

	channelsNode.Content = append(
		channelsNode.Content,
		newImplicitWeixinChannelNode(),
	)
	return true, nil
}

func shouldInjectImplicitWeixinChannel(
	channelsNode *yaml.Node,
) (bool, error) {
	if channelsNode == nil {
		return false, nil
	}

	hasWeCom := false
	for index, channelNode := range channelsNode.Content {
		if channelNode == nil ||
			channelNode.Kind != yaml.MappingNode {
			continue
		}

		typeName := configuredChannelTypeName(channelNode)
		if typeName == channelTypeWeixin {
			return false, nil
		}
		if typeName != channelTypeWeCom {
			continue
		}

		enabled, _, err := resolveConfiguredChannelEnabled(
			channelNode,
		)
		if err != nil {
			return false, fmt.Errorf(
				"config: channels[%d]: %w",
				index,
				err,
			)
		}
		if enabled {
			hasWeCom = true
		}
	}
	return hasWeCom, nil
}

func configuredChannelTypeName(channelNode *yaml.Node) string {
	return strings.ToLower(strings.TrimSpace(
		mappingStringValue(channelNode, channelTypeKey),
	))
}

func newImplicitWeixinChannelNode() *yaml.Node {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
	}
	setMappingString(node, channelTypeKey, channelTypeWeixin)
	setMappingString(node, toolNameKey, implicitWeixinChannelName)
	return node
}
