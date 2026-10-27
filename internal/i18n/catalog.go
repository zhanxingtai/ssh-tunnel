package i18n

// catalog holds the messages of one locale.
type catalog map[string]string

// catalogs holds the messages of every supported locale.
var catalogs = map[string]catalog{
	ZhCN: catalogZh,
	EnUS: catalogEn,
}
