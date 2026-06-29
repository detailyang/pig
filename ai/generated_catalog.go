package ai

import _ "embed"

//go:embed models.generated.json
var vendoredGeneratedModelCatalog []byte

var BUILTIN_MODELS = vendoredGeneratedModelCatalog

var BUILTINMODELS = BUILTIN_MODELS

func VendoredGeneratedModelCatalog() []byte {
	return append([]byte(nil), vendoredGeneratedModelCatalog...)
}

func ParseVendoredGeneratedModelCatalog() ([]Model, error) {
	return ParseModelCatalog(vendoredGeneratedModelCatalog)
}

func RegisterVendoredGeneratedModelCatalog() ([]Model, error) {
	return RegisterModelCatalog(vendoredGeneratedModelCatalog, CatalogRegistrationBuiltin)
}
