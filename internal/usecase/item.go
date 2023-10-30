package usecase

import (
	"context"

	"github.com/AJackTi/go-clean-architecture/internal/entity"
)

// ItemUseCase -.
type ItemUseCase struct {
	Store *entity.ItemStore
}

type CreateItemRequest struct {
}

func NewItemUseCase(store *entity.ItemStore) *ItemUseCase {
	return &ItemUseCase{Store: store}
}

func (uc *ItemUseCase) Create(ctx context.Context, request *CreateItemRequest) (*entity.Item, error) {
	return uc.Store.Create(&entity.Item{})
}

func (uc *ItemUseCase) CreateItem(ctx context.Context, request *CreateItemRequest) (*entity.Item, error) {
	return uc.Store.Create(&entity.Item{})
}
