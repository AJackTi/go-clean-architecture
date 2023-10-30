package v2

import (
	"net/http"
	"strconv"
	"time"

	"github.com/AJackTi/go-clean-architecture/internal/middleware"

	"github.com/AJackTi/go-clean-architecture/config"
	"github.com/AJackTi/go-clean-architecture/internal/usecase"
	"github.com/AJackTi/go-clean-architecture/pkg/graph"
	"github.com/AJackTi/go-clean-architecture/pkg/notification"
	"github.com/AJackTi/go-clean-architecture/pkg/ssehandler"

	"github.com/AJackTi/go-clean-architecture/internal/common"
	"github.com/gin-gonic/gin"
)

type missionRoutes struct {
	missionUc               *usecase.MissionUseCase
	actionDataUc            *usecase.ActionDataUseCase
	metaDataUc              *usecase.MetadataUseCase
	actionUc                *usecase.ActionUseCase
	actionMissionUc         *usecase.ActionMissionUseCase
	userUc                  *usecase.UserUseCase
	userTimekeeperUc        *usecase.UserTimekeeperUseCase
	entryUc                 *usecase.EntryUseCase
	walletUc                *usecase.WalletUseCase
	notificationUc          *usecase.NotificationUseCase
	notificationRecipientUc *usecase.NotificationRecipientUseCase
	notification            *notification.Notification
	cfg                     *config.Config
	graph                   *graph.Graph
	sseHandler              *ssehandler.SSEHandler
}

func (hand *handler) NewMissionRoutes(handler *gin.RouterGroup) *handler {
	r := &missionRoutes{
		missionUc:               hand.missionUc,
		actionDataUc:            hand.actionDataUc,
		metaDataUc:              hand.metaDataUc,
		actionUc:                hand.actionUc,
		actionMissionUc:         hand.actionMissionUc,
		userUc:                  hand.userUc,
		userTimekeeperUc:        hand.userTimekeeperUc,
		entryUc:                 hand.entryUc,
		walletUc:                hand.walletUc,
		notificationUc:          hand.notificationUc,
		notificationRecipientUc: hand.notificationRecipientUc,
		notification:            hand.notification,
		cfg:                     hand.cfg,
		graph:                   hand.graph,
		sseHandler:              hand.sseHandler,
	}

	h := handler.Group("/mission")
	{
		h.Use(middleware.TimezoneDataHeader).GET("", r.ListMissionsByAddress)
	}

	return hand
}

// @Summary     List missions of wallet address
// @Description List missions of wallet address
// @ID          list-missions
// @Tags  	    list-missions
// @Accept      json
// @Produce     json
// @Param       address   path      string  true  "Public address of wallet"
// @Param       limit   path        string  true  "Public address of wallet"
// @Success     200
// @Failure     500
// @Router      /missions [get]
func (r *missionRoutes) ListMissionsByAddress(c *gin.Context) {
	var (
		limit int64 = 5
		err   error
		loc   = common.GetLoc(c)
		now   = time.Now().In(loc)
	)

	limitQuery := c.Query("limit")
	if len(limitQuery) != 0 {
		limit, err = strconv.ParseInt(limitQuery, 10, 64)
		if err != nil {
			common.ErrorResponse(c, http.StatusInternalServerError, "Invalid data type limit!", err.Error())

			return
		}
	}

	address := c.Query("address")
	if len(address) == 0 {
		common.ErrorResponse(c, http.StatusBadRequest, "Missing param", "Missing param")

		return
	}

	missions, err := r.missionUc.ListMissionsAddressV2(c.Request.Context(), address, &now, limit, loc)
	if err != nil {
		common.ErrorResponse(c, http.StatusInternalServerError, "Failed to list mission.", err.Error())
		return
	}

	c.JSON(http.StatusOK, missions)
}
