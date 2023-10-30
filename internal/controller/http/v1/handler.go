package v1

import (
	"github.com/AJackTi/go-clean-architecture/config"
	"github.com/AJackTi/go-clean-architecture/internal/usecase"
	"github.com/AJackTi/go-clean-architecture/pkg/aws"
	"github.com/AJackTi/go-clean-architecture/pkg/graph"
	"github.com/AJackTi/go-clean-architecture/pkg/notification"
	sseHandler "github.com/AJackTi/go-clean-architecture/pkg/sse"
)

type handler struct {
	itemUc       *usecase.ItemUseCase
	cfg          *config.Config
	graph        *graph.Graph
	notification *notification.Notification
	sseHandler   *sseHandler.SSEHandler
	s3Aws        *aws.S3
}

func New(itemUc *usecase.ItemUseCase,
	cfg *config.Config,
	graph *graph.Graph,
	notificationModel *notification.Notification,
	sseHandler *sseHandler.SSEHandler,
	s3 *aws.S3) *handler {
	return &handler{
		itemUc:       itemUc,
		cfg:          cfg,
		graph:        graph,
		notification: notificationModel,
		sseHandler:   sseHandler,
		s3Aws:        s3,
	}
}
