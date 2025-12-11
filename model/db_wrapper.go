package model

import "gorm.io/gorm"

// DBWrapper is a thin type alias around gorm.DB used only by
// integration tests under scene_test to express direct DB access.
// Alias keeps runtime behavior identical to *gorm.DB while allowing
// tests to depend on a stable exported type.
type DBWrapper = gorm.DB
