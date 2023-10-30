package v2

import (
	"github.com/AJackTi/go-clean-architecture/config"
	"github.com/AJackTi/go-clean-architecture/internal/usecase"
	"github.com/AJackTi/go-clean-architecture/pkg/aws"
	"github.com/AJackTi/go-clean-architecture/pkg/graph"
	"github.com/AJackTi/go-clean-architecture/pkg/notification"
	"github.com/AJackTi/go-clean-architecture/pkg/ssehandler"
)

type handler struct {
	userUc                  *usecase.UserUseCase
	walletUc                *usecase.WalletUseCase
	missionUc               *usecase.MissionUseCase
	actionDataUc            *usecase.ActionDataUseCase
	metaDataUc              *usecase.MetadataUseCase
	actionUc                *usecase.ActionUseCase
	actionMissionUc         *usecase.ActionMissionUseCase
	levelUc                 *usecase.LevelUseCase
	officeUc                *usecase.OfficeUseCase
	membershipUc            *usecase.MembershipUseCase
	userMembershipUc        *usecase.UserMembershipUseCase
	userTimekeeperUc        *usecase.UserTimekeeperUseCase
	entryUc                 *usecase.EntryUseCase
	notificationUc          *usecase.NotificationUseCase
	notificationRecipientUc *usecase.NotificationRecipientUseCase
	productUc               *usecase.ProductUseCase
	productCategoryUc       *usecase.ProductCategoryUseCase
	orderUc                 *usecase.OrderUseCase
	orderItemUc             *usecase.OrderItemUseCase
	eventUc                 *usecase.EventUseCase
	commentUc               *usecase.CommentUseCase
	newsUc                  *usecase.NewsUseCase
	configUc                *usecase.ConfigUseCase
	cfg                     *config.Config
	graph                   *graph.Graph
	notification            *notification.Notification
	sseHandler              *ssehandler.SSEHandler
	s3Aws                   *aws.S3
}

func New(
	missionUc *usecase.MissionUseCase,
	actionMissionUc *usecase.ActionMissionUseCase) *handler {
	return &handler{
		missionUc:       missionUc,
		actionMissionUc: actionMissionUc,
	}
}
