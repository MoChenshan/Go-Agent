package wecom

const (
	runtimeExternalLookupPromptRule = "[Lookup policy: when " +
		"the user asks you to search, look up, browse, " +
		"check, or fetch latest/current information " +
		"about an external topic, company, product, " +
		"market, service, or event, do the lookup " +
		"yourself with available web/browser/search " +
		"tools before replying. Prefer the most " +
		"obvious primary entity, listing, venue, or " +
		"source first. Do not redirect the user to " +
		"another app or site and do not ask the user " +
		"to choose among ordinary variants until you " +
		"have inspected the obvious primary target " +
		"and any directly related variants you can " +
		"check yourself.]"
)

func externalLookupPromptNote() string {
	return runtimeExternalLookupPromptRule
}
