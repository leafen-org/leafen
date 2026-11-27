package app

const databaseRelationValueAuthoritySchemaVersion = 3

func databaseRelationStrings(values []any) ([]string, error) {
	result := make([]string, len(values))
	for index, value := range values {
		text, ok := value.(string)
		if !ok {
			return nil, graphStructureAuthorityError()
		}
		result[index] = text
	}
	return result, nil
}
