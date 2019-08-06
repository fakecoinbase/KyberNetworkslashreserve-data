package http

import (
	"errors"
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/getsentry/raven-go"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/sentry"
	"github.com/gin-gonic/gin"

	"github.com/KyberNetwork/reserve-data"
	"github.com/KyberNetwork/reserve-data/cmd/deployment"
	"github.com/KyberNetwork/reserve-data/common"
	"github.com/KyberNetwork/reserve-data/http/httputil"
	"github.com/KyberNetwork/reserve-data/metric"
	v3common "github.com/KyberNetwork/reserve-data/v3/common"
	v3http "github.com/KyberNetwork/reserve-data/v3/http"
	"github.com/KyberNetwork/reserve-data/v3/storage"
)

const (
	maxTimespot uint64 = 18446744073709551615
	maxDataSize int    = 1000000 //1 Megabyte in byte
)

var (
	// errDataSizeExceed is returned when the post data is larger than maxDataSize.
	errDataSizeExceed = errors.New("the data size must be less than 1 MB")
)

// Server struct for http package
type Server struct {
	app                 reserve.Data
	core                reserve.Core
	metric              metric.Storage
	host                string
	r                   *gin.Engine
	blockchain          Blockchain
	contractAddressConf *common.ContractAddressConfiguration
	settingStorage      storage.Interface
}

func getTimePoint(c *gin.Context, useDefault bool) uint64 {
	timestamp := c.DefaultQuery("timestamp", "")
	if timestamp == "" {
		if useDefault {
			log.Printf("Interpreted timestamp to default - %d\n", maxTimespot)
			return maxTimespot
		}
		timepoint := common.GetTimepoint()
		log.Printf("Interpreted timestamp to current time - %d\n", timepoint)
		return timepoint
	}
	timepoint, err := strconv.ParseUint(timestamp, 10, 64)
	if err != nil {
		log.Printf("Interpreted timestamp(%s) to default - %d", timestamp, maxTimespot)
		return maxTimespot
	}
	log.Printf("Interpreted timestamp(%s) to %d", timestamp, timepoint)
	return timepoint
}

// IsIntime is a part of authentication check if the request is new
func IsIntime(nonce string) bool {
	serverTime := common.GetTimepoint()
	log.Printf("Server time: %d, None: %s", serverTime, nonce)
	nonceInt, err := strconv.ParseInt(nonce, 10, 64)
	if err != nil {
		log.Printf("IsIntime returns false, err: %v", err)
		return false
	}
	difference := nonceInt - int64(serverTime)
	if difference < -30000 || difference > 30000 {
		log.Printf("IsIntime returns false, nonce: %d, serverTime: %d, difference: %d", nonceInt, int64(serverTime), difference)
		return false
	}
	return true
}

// AllPricesVersion return current version of all token
func (s *Server) AllPricesVersion(c *gin.Context) {
	log.Printf("Getting all prices version")
	data, err := s.app.CurrentPriceVersion(getTimePoint(c, true))
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
	} else {
		httputil.ResponseSuccess(c, httputil.WithField("version", data))
	}
}

type price struct {
	Base     uint64              `json:"base"`
	Quote    uint64              `json:"quote"`
	Exchange uint64              `json:"exchange"`
	Bids     []common.PriceEntry `json:"bids"`
	Asks     []common.PriceEntry `json:"asks"`
}

func (s *Server) AllPrices(c *gin.Context) {
	log.Printf("Getting all prices \n")
	data, err := s.app.GetAllPrices(getTimePoint(c, true))
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}

	var responseData []price
	for tp, onePrice := range data.Data {
		pair, err := s.settingStorage.GetTradingPair(tp)
		if err != nil {
			httputil.ResponseFailure(c, httputil.WithError(err))
			return
		}
		for exchangeName, exchangePrice := range onePrice {
			exchangeID, err := common.GetExchange(string(exchangeName))
			if err != nil {
				httputil.ResponseFailure(c, httputil.WithError(err))
				return
			}
			// TODO should we check exchangeID.Name() match pair.ExchangeID?
			responseData = append(responseData, price{
				Base:     pair.Base,
				Quote:    pair.Quote,
				Exchange: uint64(exchangeID.Name()),
				Bids:     exchangePrice.Bids,
				Asks:     exchangePrice.Asks,
			})
		}
	}

	httputil.ResponseSuccess(c, httputil.WithMultipleFields(gin.H{
		"version":   data.Version,
		"timestamp": data.Timestamp,
		"data":      responseData,
		"block":     data.Block,
	}))

}

// Price return price of a token
func (s *Server) Price(c *gin.Context) {
	base := c.Param("base")
	quote := c.Param("quote")
	log.Printf("Getting price for %s - %s \n", base, quote)
	// TODO: change getting price to accept asset id
	//pair, err := s.setting.NewTokenPairFromID(base, quote)
	//if err != nil {
	//	httputil.ResponseFailure(c, httputil.WithReason("Token pair is not supported"))
	//} else {
	//	data, err := s.app.GetOnePrice(pair.PairID(), getTimePoint(c, true))
	//	if err != nil {
	//		httputil.ResponseFailure(c, httputil.WithError(err))
	//	} else {
	//		httputil.ResponseSuccess(c, httputil.WithMultipleFields(gin.H{
	//			"version":   data.Version,
	//			"timestamp": data.Timestamp,
	//			"exchanges": data.Data,
	//		}))
	//	}
	//}
}

// AuthDataVersion return current version of auth data
func (s *Server) AuthDataVersion(c *gin.Context) {
	log.Printf("Getting current auth data snapshot version")
	data, err := s.app.CurrentAuthDataVersion(getTimePoint(c, true))
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
	} else {
		httputil.ResponseSuccess(c, httputil.WithField("version", data))
	}
}

// AuthData return current auth data
func (s *Server) AuthData(c *gin.Context) {
	log.Printf("Getting current auth data snapshot \n")
	data, err := s.app.GetAuthData(getTimePoint(c, true))
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
	} else {
		httputil.ResponseSuccess(c, httputil.WithMultipleFields(gin.H{
			"version": data.Version,
			"data":    data,
		}))
	}
}

// GetRates return all rates
func (s *Server) GetRates(c *gin.Context) {
	log.Printf("Getting all rates \n")
	fromTime, _ := strconv.ParseUint(c.Query("fromTime"), 10, 64)
	toTime, _ := strconv.ParseUint(c.Query("toTime"), 10, 64)
	if toTime == 0 {
		toTime = maxTimespot
	}
	data, err := s.app.GetRates(fromTime, toTime)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
	} else {
		httputil.ResponseSuccess(c, httputil.WithData(data))
	}
}

// GetRate return rate of a token
func (s *Server) GetRate(c *gin.Context) {
	log.Printf("Getting all rates \n")
	data, err := s.app.GetRate(getTimePoint(c, true))
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
	} else {
		httputil.ResponseSuccess(c, httputil.WithMultipleFields(gin.H{
			"version":   data.Version,
			"timestamp": data.Timestamp,
			"data":      data.Data,
		}))
	}
}

// RateRequest is request for a rate
type RateRequest struct {
	AssetID uint64 `json:"asset_id"`
	Buy     string `json:"buy"`
	Sell    string `json:"sell"`
	Mid     string `json:"mid"`
	Msg     string `json:"msg"`
}

// SetRateEntry is input for set rate request
type SetRateEntry struct {
	Block uint64        `json:"block"`
	Rates []RateRequest `json:"rates"`
}

// SetRate is for setting token rate
func (s *Server) SetRate(c *gin.Context) {
	var (
		input     SetRateEntry
		assets    []v3common.Asset
		bigBuys   = []*big.Int{}
		bigSells  = []*big.Int{}
		bigAfpMid = []*big.Int{}
		msgs      []string
	)
	if err := c.ShouldBindJSON(&input); err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
	}
	for _, rates := range input.Rates {
		asset, err := s.settingStorage.GetAsset(rates.AssetID)
		if err != nil {
			httputil.ResponseFailure(c, httputil.WithError(err))
			return
		}
		assets = append(assets, asset)
	}
	for _, rate := range input.Rates {
		rbuy, ok := big.NewInt(0).SetString(rate.Buy, 10)
		if !ok {
			httputil.ResponseFailure(c, httputil.WithError(fmt.Errorf("cannot parse rate number buy: %s", rate.Buy)))
			return
		}
		bigBuys = append(bigBuys, rbuy)
		rSell, ok := big.NewInt(0).SetString(rate.Sell, 10)
		if !ok {
			httputil.ResponseFailure(c, httputil.WithError(fmt.Errorf("cannot parse rate number sell: %s", rate.Sell)))
			return
		}
		bigSells = append(bigSells, rSell)
		rMid, ok := big.NewInt(0).SetString(rate.Mid, 10)
		if !ok {
			httputil.ResponseFailure(c, httputil.WithError(fmt.Errorf("cannot parse rate number mid: %s", rate.Mid)))
			return
		}
		bigAfpMid = append(bigAfpMid, rMid)
		msgs = append(msgs, rate.Msg)
	}
	id, err := s.core.SetRates(assets, bigBuys, bigSells, big.NewInt(int64(input.Block)), bigAfpMid, msgs)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	httputil.ResponseSuccess(c, httputil.WithField("id", id))
}

// Trade create an order in cexs
func (s *Server) Trade(c *gin.Context) {
	postForm := c.Request.Form
	exchangeParam := c.Param("exchangeid")
	pairIDParam := c.Param("pair")
	amountParam := postForm.Get("amount")
	rateParam := postForm.Get("rate")
	typeParam := postForm.Get("type")

	pairID, err := strconv.Atoi(pairIDParam)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(fmt.Errorf("invalid pair id %s err=%s", pairIDParam, err.Error())))
		return
	}

	exchange, err := common.GetExchange(exchangeParam)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	//TODO: use GetTradingPair method
	var pair v3common.TradingPairSymbols
	pairs, err := s.settingStorage.GetTradingPairs(uint64(exchange.Name()))
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	for _, p := range pairs {
		if p.ID == uint64(pairID) {
			pair = p
		}
	}
	amount, err := strconv.ParseFloat(amountParam, 64)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	rate, err := strconv.ParseFloat(rateParam, 64)
	log.Printf("http server: Trade: rate: %f, raw rate: %s", rate, rateParam)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	if typeParam != "sell" && typeParam != "buy" {
		httputil.ResponseFailure(c, httputil.WithReason(fmt.Sprintf("Trade type of %s is not supported.", typeParam)))
		return
	}

	id, done, remaining, finished, err := s.core.Trade(
		exchange, typeParam, pair, rate, amount, getTimePoint(c, false))
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	httputil.ResponseSuccess(c, httputil.WithMultipleFields(gin.H{
		"id":        id,
		"done":      done,
		"remaining": remaining,
		"finished":  finished,
	}))
}

// CancelOrder cancel an order from cexs
func (s *Server) CancelOrder(c *gin.Context) {
	postForm := c.Request.Form
	exchangeParam := c.Param("exchangeid")
	id := postForm.Get("order_id")

	exchange, err := common.GetExchange(exchangeParam)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	log.Printf("Cancel order id: %s from %s\n", id, exchange.ID())
	activityID, err := common.StringToActivityID(id)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	err = s.core.CancelOrder(activityID, exchange)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	httputil.ResponseSuccess(c)
}

// Withdraw asset to reserve from cex
func (s *Server) Withdraw(c *gin.Context) {
	postForm := c.Request.Form
	exchangeParam := c.Param("exchangeid")
	assetParam := postForm.Get("asset")
	amountParam := postForm.Get("amount")

	exchange, err := common.GetExchange(exchangeParam)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}

	assetID, err := strconv.Atoi(assetParam)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	asset, err := s.settingStorage.GetAsset(uint64(assetID))
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	amount, err := hexutil.DecodeBig(amountParam)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	log.Printf("Withdraw %s %d from %s\n", amount.Text(10), asset.ID, exchange.ID())
	id, err := s.core.Withdraw(exchange, asset, amount, getTimePoint(c, false))
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	httputil.ResponseSuccess(c, httputil.WithField("id", id))
}

// Deposit asset into cex
func (s *Server) Deposit(c *gin.Context) {
	postForm := c.Request.Form
	exchangeParam := c.Param("exchangeid")
	amountParam := postForm.Get("amount")
	assetIDParam := postForm.Get("asset")
	assetID, err := strconv.Atoi(assetIDParam)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}

	exchange, err := common.GetExchange(exchangeParam)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	asset, err := s.settingStorage.GetAsset(uint64(assetID))
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	amount, err := hexutil.DecodeBig(amountParam)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}

	log.Printf("Depositing %s %d to %s\n", amount.Text(10), asset.ID, exchange.ID())
	id, err := s.core.Deposit(exchange, asset, amount, getTimePoint(c, false))
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	httputil.ResponseSuccess(c, httputil.WithField("id", id))
}

// GetActivities return all activities record
func (s *Server) GetActivities(c *gin.Context) {
	log.Printf("Getting all activity records \n")
	fromTime, _ := strconv.ParseUint(c.Query("fromTime"), 10, 64)
	toTime, _ := strconv.ParseUint(c.Query("toTime"), 10, 64)
	if toTime == 0 {
		toTime = common.GetTimepoint()
	}

	data, err := s.app.GetRecords(fromTime*1000000, toTime*1000000)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
	} else {
		httputil.ResponseSuccess(c, httputil.WithData(data))
	}
}

// StopFetcher stop fetcher from fetch data
func (s *Server) StopFetcher(c *gin.Context) {
	err := s.app.Stop()
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
	} else {
		httputil.ResponseSuccess(c)
	}
}

// ImmediatePendingActivities return activities which are pending
func (s *Server) ImmediatePendingActivities(c *gin.Context) {
	log.Printf("Getting all immediate pending activity records \n")
	data, err := s.app.GetPendingActivities()
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
	} else {
		httputil.ResponseSuccess(c, httputil.WithData(data))
	}
}

// Metrics return all metrics
func (s *Server) Metrics(c *gin.Context) {
	response := common.MetricResponse{
		Timestamp: common.GetTimepoint(),
	}
	log.Printf("Getting metrics")
	postForm := c.Request.Form
	assetIDsParam := postForm.Get("assets")
	fromParam := postForm.Get("from")
	toParam := postForm.Get("to")
	var assets []v3common.Asset
	for _, assetID := range strings.Split(assetIDsParam, "-") {
		id, err := strconv.Atoi(assetID)
		if err != nil {
			httputil.ResponseFailure(c, httputil.WithError(err))
		}
		asset, err := s.settingStorage.GetAsset(uint64(id))
		if err != nil {
			httputil.ResponseFailure(c, httputil.WithError(err))
			return
		}
		assets = append(assets, asset)
	}
	from, err := strconv.ParseUint(fromParam, 10, 64)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
	}
	to, err := strconv.ParseUint(toParam, 10, 64)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
	}
	data, err := s.metric.GetMetric(assets, from, to)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
	}
	response.ReturnTime = common.GetTimepoint()
	response.Data = data
	httputil.ResponseSuccess(c, httputil.WithMultipleFields(gin.H{
		"timestamp":  response.Timestamp,
		"returnTime": response.ReturnTime,
		"data":       response.Data,
	}))
}

// StoreMetrics store metrics into db
func (s *Server) StoreMetrics(c *gin.Context) {
	log.Printf("Storing metrics")
	postForm := c.Request.Form
	timestampParam := postForm.Get("timestamp")
	dataParam := postForm.Get("data")

	timestamp, err := strconv.ParseUint(timestampParam, 10, 64)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	metricEntry := common.MetricEntry{}
	metricEntry.Timestamp = timestamp
	metricEntry.Data = map[uint64]common.TokenMetric{}
	// data must be in form of <token>_afpmid_spread|<token>_afpmid_spread|...
	for _, tokenData := range strings.Split(dataParam, "|") {
		var (
			afpmid float64
			spread float64
		)

		parts := strings.Split(tokenData, "_")
		if len(parts) != 3 {
			httputil.ResponseFailure(c, httputil.WithReason("submitted data is not in correct format"))
			return
		}
		assetIDStr := parts[0]
		assetID, err := strconv.Atoi(assetIDStr)
		if err != nil {
			httputil.ResponseFailure(c, httputil.WithError(err))
			return
		}
		afpmidStr := parts[1]
		spreadStr := parts[2]

		if afpmid, err = strconv.ParseFloat(afpmidStr, 64); err != nil {
			httputil.ResponseFailure(c, httputil.WithReason("Afp mid "+afpmidStr+" is not float64"))
			return
		}

		if spread, err = strconv.ParseFloat(spreadStr, 64); err != nil {
			httputil.ResponseFailure(c, httputil.WithReason("Spread "+spreadStr+" is not float64"))
			return
		}
		metricEntry.Data[uint64(assetID)] = common.TokenMetric{
			AfpMid: afpmid,
			Spread: spread,
		}
	}

	err = s.metric.StoreMetric(&metricEntry, common.GetTimepoint())
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
	} else {
		httputil.ResponseSuccess(c)
	}
}

//ValidateExchangeInfo validate if data is complete exchange info with all token pairs supported
// func ValidateExchangeInfo(exchange common.Exchange, data map[common.TokenPairID]common.ExchangePrecisionLimit) error {
// 	exInfo, err :=self
// 	pairs := exchange.Pairs()
// 	for _, pair := range pairs {
// 		// stable exchange is a simulated exchange which is not a real exchange
// 		// we do not do rebalance on stable exchange then it also does not need to have exchange info (and it actully does not have one)
// 		// therefore we skip checking it for supported tokens
// 		if exchange.ID() == common.ExchangeID("stable_exchange") {
// 			continue
// 		}
// 		if _, exist := data[pair.PairID()]; !exist {
// 			return fmt.Errorf("exchange info of %s lack of token %s", exchange.ID(), string(pair.PairID()))
// 		}
// 	}
// 	return nil
// }

// GetTradeHistory return trade history
func (s *Server) GetTradeHistory(c *gin.Context) {
	fromTime, toTime, ok := s.ValidateTimeInput(c)
	if !ok {
		return
	}
	data, err := s.app.GetTradeHistory(fromTime, toTime)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
	}
	httputil.ResponseSuccess(c, httputil.WithData(data))
}

// GetTimeServer return server time
func (s *Server) GetTimeServer(c *gin.Context) {
	httputil.ResponseSuccess(c, httputil.WithData(common.GetTimestamp()))
}

// GetRebalanceStatus return rebalanceStatus (true or false)
func (s *Server) GetRebalanceStatus(c *gin.Context) {
	data, err := s.metric.GetRebalanceControl()
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	httputil.ResponseSuccess(c, httputil.WithData(data.Status))
}

//HoldRebalance stop rebalance action
func (s *Server) HoldRebalance(c *gin.Context) {
	if err := s.metric.StoreRebalanceControl(false); err != nil {
		httputil.ResponseFailure(c, httputil.WithReason(err.Error()))
		return
	}
	httputil.ResponseSuccess(c)
}

// EnableRebalance enable rebalance
func (s *Server) EnableRebalance(c *gin.Context) {
	if err := s.metric.StoreRebalanceControl(true); err != nil {
		httputil.ResponseFailure(c, httputil.WithReason(err.Error()))
	}
	httputil.ResponseSuccess(c)
}

// GetSetrateStatus return setRateStatus (true or false)
func (s *Server) GetSetrateStatus(c *gin.Context) {
	data, err := s.metric.GetSetrateControl()
	if err != nil {
		httputil.ResponseFailure(c)
		return
	}
	httputil.ResponseSuccess(c, httputil.WithData(data.Status))
}

// HoldSetrate stop set rate
func (s *Server) HoldSetrate(c *gin.Context) {
	if err := s.metric.StoreSetrateControl(false); err != nil {
		httputil.ResponseFailure(c, httputil.WithReason(err.Error()))
	}
	httputil.ResponseSuccess(c)
}

// EnableSetrate allow analytics to call setrate
func (s *Server) EnableSetrate(c *gin.Context) {
	if err := s.metric.StoreSetrateControl(true); err != nil {
		httputil.ResponseFailure(c, httputil.WithReason(err.Error()))
	}
	httputil.ResponseSuccess(c)
}

// ValidateTimeInput check if the params fromTime, toTime is valid or not
func (s *Server) ValidateTimeInput(c *gin.Context) (uint64, uint64, bool) {
	fromTime, ok := strconv.ParseUint(c.Query("fromTime"), 10, 64)
	if ok != nil {
		httputil.ResponseFailure(c, httputil.WithReason(fmt.Sprintf("fromTime param is invalid: %s", ok)))
		return 0, 0, false
	}
	toTime, _ := strconv.ParseUint(c.Query("toTime"), 10, 64)
	if toTime == 0 {
		toTime = common.GetTimepoint()
	}
	return fromTime, toTime, true
}

// SetStableTokenParams set stable token params
func (s *Server) SetStableTokenParams(c *gin.Context) {
	postForm := c.Request.Form
	value := []byte(postForm.Get("value"))
	if len(value) > maxDataSize {
		httputil.ResponseFailure(c, httputil.WithReason(errDataSizeExceed.Error()))
		return
	}
	err := s.metric.SetStableTokenParams(value)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	httputil.ResponseSuccess(c)
}

// ConfirmStableTokenParams confirm stable token params
func (s *Server) ConfirmStableTokenParams(c *gin.Context) {
	postForm := c.Request.Form
	value := []byte(postForm.Get("value"))
	if len(value) > maxDataSize {
		httputil.ResponseFailure(c, httputil.WithReason(errDataSizeExceed.Error()))
		return
	}
	err := s.metric.ConfirmStableTokenParams(value)
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	httputil.ResponseSuccess(c)
}

// RejectStableTokenParams reject stable token params
func (s *Server) RejectStableTokenParams(c *gin.Context) {
	err := s.metric.RemovePendingStableTokenParams()
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	httputil.ResponseSuccess(c)
}

// GetPendingStableTokenParams return pending stable token params
func (s *Server) GetPendingStableTokenParams(c *gin.Context) {
	data, err := s.metric.GetPendingStableTokenParams()
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	httputil.ResponseSuccess(c, httputil.WithData(data))
}

// GetStableTokenParams return all stable token params
func (s *Server) GetStableTokenParams(c *gin.Context) {
	data, err := s.metric.GetStableTokenParams()
	if err != nil {
		httputil.ResponseFailure(c, httputil.WithError(err))
		return
	}
	httputil.ResponseSuccess(c, httputil.WithData(data))
}

func (s *Server) register() {
	if s.core != nil && s.app != nil {
		s.r.GET("/prices-version", s.AllPricesVersion)
		s.r.GET("/prices", s.AllPrices)
		s.r.GET("/prices/:base/:quote", s.Price)
		s.r.GET("/getrates", s.GetRate)
		s.r.GET("/get-all-rates", s.GetRates)

		s.r.GET("/authdata-version", s.AuthDataVersion)
		s.r.GET("/authdata", s.AuthData)
		s.r.GET("/activities", s.GetActivities)
		s.r.GET("/immediate-pending-activities", s.ImmediatePendingActivities)
		s.r.GET("/metrics", s.Metrics)
		s.r.POST("/metrics", s.StoreMetrics)

		s.r.POST("/cancelorder/:exchangeid", s.CancelOrder)
		s.r.POST("/deposit/:exchangeid", s.Deposit)
		s.r.POST("/withdraw/:exchangeid", s.Withdraw)
		s.r.POST("/trade/:exchangeid", s.Trade)
		s.r.POST("/setrates", s.SetRate)
		s.r.GET("/tradehistory", s.GetTradeHistory)

		s.r.GET("/timeserver", s.GetTimeServer)

		s.r.GET("/rebalancestatus", s.GetRebalanceStatus)
		s.r.POST("/holdrebalance", s.HoldRebalance)
		s.r.POST("/enablerebalance", s.EnableRebalance)

		s.r.GET("/setratestatus", s.GetSetrateStatus)
		s.r.POST("/holdsetrate", s.HoldSetrate)
		s.r.POST("/enablesetrate", s.EnableSetrate)

		s.r.POST("/set-stable-token-params", s.SetStableTokenParams)
		s.r.POST("/confirm-stable-token-params", s.ConfirmStableTokenParams)
		s.r.POST("/reject-stable-token-params", s.RejectStableTokenParams)
		s.r.GET("/pending-stable-token-params", s.GetPendingStableTokenParams)
		s.r.GET("/stable-token-params", s.GetStableTokenParams)

		s.r.GET("/gold-feed", s.GetGoldData)
		s.r.GET("/btc-feed", s.GetBTCData)
		s.r.POST("/set-feed-configuration", s.UpdateFeedConfiguration)
		s.r.GET("/get-feed-configuration", s.GetFeedConfiguration)

		_ = v3http.NewServer(s.settingStorage, s.r) // ignore server object because we just use the route part
	}
}

// Run the server
func (s *Server) Run() {
	s.register()
	if err := s.r.Run(s.host); err != nil {
		log.Panic(err)
	}
}

// NewHTTPServer return new server
func NewHTTPServer(
	app reserve.Data,
	core reserve.Core,
	metric metric.Storage,
	host string,
	dpl deployment.Deployment,
	bc Blockchain,
	contractAddressConf *common.ContractAddressConfiguration,
	settingStorage storage.Interface,
) *Server {
	r := gin.Default()
	sentryCli, err := raven.NewWithTags(
		"https://bf15053001464a5195a81bc41b644751:eff41ac715114b20b940010208271b13@sentry.io/228067",
		map[string]string{
			"env": dpl.String(),
		},
	)
	if err != nil {
		panic(err)
	}
	r.Use(sentry.Recovery(
		sentryCli,
		false,
	))
	corsConfig := cors.DefaultConfig()
	corsConfig.AddAllowHeaders("signed")
	corsConfig.AllowAllOrigins = true
	corsConfig.MaxAge = 5 * time.Minute
	r.Use(cors.New(corsConfig))

	return &Server{
		app:                 app,
		core:                core,
		metric:              metric,
		host:                host,
		r:                   r,
		blockchain:          bc,
		contractAddressConf: contractAddressConf,
		settingStorage:      settingStorage,
	}
}
