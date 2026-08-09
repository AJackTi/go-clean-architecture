package v1

import (
	"github.com/AJackTi/go-clean-architecture/internal/usecase"
)

type handler struct {
	itemUc *usecase.ItemUseCase
}

func New(itemUc *usecase.ItemUseCase) *handler {
	return &handler{
		itemUc: itemUc,
	}
}
