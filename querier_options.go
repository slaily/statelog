package statelog

type querierConfig struct {
	indexNames []string
}

func defaultQuerierConfig() querierConfig {
	return querierConfig{}
}

// QuerierOption configures a Querier instance.
type QuerierOption func(*querierConfig)

// Index registers a named index on a struct field. During the initial scan,
// the field's raw bytes are hashed and stored for O(log n) lookups.
func Index(fieldName string) QuerierOption {
	return func(c *querierConfig) {
		c.indexNames = append(c.indexNames, fieldName)
	}
}
