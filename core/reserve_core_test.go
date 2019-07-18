package core

import (
	"math/big"
	"testing"

	ethereum "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/KyberNetwork/reserve-data/common"
	commonv3 "github.com/KyberNetwork/reserve-data/v3/common"
)

type testExchange struct {
}

func (te testExchange) ID() common.ExchangeID {
	return "bittrex"
}

func (te testExchange) Address(token common.Token) (address ethereum.Address, supported bool) {
	return ethereum.Address{}, true
}
func (te testExchange) Withdraw(token common.Token, amount *big.Int, address ethereum.Address, timepoint uint64) (string, error) {
	return "withdrawid", nil
}
func (te testExchange) Trade(tradeType string, base common.Token, quote common.Token, rate float64, amount float64, timepoint uint64) (id string, done float64, remaining float64, finished bool, err error) {
	return "tradeid", 10, 5, false, nil
}
func (te testExchange) CancelOrder(id string, base, quote string) error {
	return nil
}
func (te testExchange) MarshalText() (text []byte, err error) {
	return []byte("bittrex"), nil
}

func (te testExchange) GetTradeHistory(fromTime, toTime uint64) (common.ExchangeTradeHistory, error) {
	return common.ExchangeTradeHistory{}, nil
}

func (te testExchange) GetLiveExchangeInfos(pairIDs []common.TokenPairID) (common.ExchangeInfo, error) {
	return common.ExchangeInfo{}, nil
}

type testBlockchain struct {
}

func (tbc testBlockchain) Send(
	asset commonv3.Asset,
	amount *big.Int,
	address ethereum.Address) (*types.Transaction, error) {
	tx := types.NewTransaction(
		0,
		ethereum.Address{},
		big.NewInt(0),
		300000,
		big.NewInt(1000000000),
		[]byte{})
	return tx, nil
}

func (tbc testBlockchain) SetRates(
	tokens []ethereum.Address,
	buys []*big.Int,
	sells []*big.Int,
	block *big.Int,
	nonce *big.Int,
	gasPrice *big.Int) (*types.Transaction, error) {
	tx := types.NewTransaction(
		0,
		ethereum.Address{},
		big.NewInt(0),
		300000,
		big.NewInt(1000000000),
		[]byte{})
	return tx, nil
}

func (tbc testBlockchain) StandardGasPrice() float64 {
	return 0
}

func (tbc testBlockchain) SetRateMinedNonce() (uint64, error) {
	return 0, nil
}

type testActivityStorage struct {
	PendingDeposit bool
}

func (tas testActivityStorage) Record(
	action string,
	id common.ActivityID,
	destination string,
	params map[string]interface{},
	result map[string]interface{},
	estatus string,
	mstatus string,
	timepoint uint64) error {
	return nil
}

func (tas testActivityStorage) GetActivity(id common.ActivityID) (common.ActivityRecord, error) {
	return common.ActivityRecord{}, nil
}

func (tas testActivityStorage) PendingSetRate(minedNonce uint64) (*common.ActivityRecord, uint64, error) {
	return nil, 0, nil
}

func (tas testActivityStorage) HasPendingDeposit(token common.Token, exchange common.Exchange) (bool, error) {
	if token.ID == "OMG" && exchange.ID() == "bittrex" {
		return tas.PendingDeposit, nil
	}
	return false, nil
}

func getTestCore(hasPendingDeposit bool) *ReserveCore {
	addressSetting := &common.ContractAddressConfiguration{}

	return NewReserveCore(
		testBlockchain{},
		testActivityStorage{hasPendingDeposit},
		addressSetting,
	)
}

func TestNotAllowDeposit(t *testing.T) {
	core := getTestCore(true)
	_, err := core.Deposit(
		testExchange{},
		common.NewToken("OMG", "omise-go", "0x1111111111111111111111111111111111111111", 18, true, true, 0),
		big.NewInt(10),
		common.GetTimepoint(),
	)
	if err == nil {
		t.Fatalf("Expected to return an error protecting user from deposit when there is another pending deposit")
	}
	_, err = core.Deposit(
		testExchange{},
		common.NewToken("KNC", "Kyber-coin", "0x1111111111111111111111111111111111111111", 18, true, true, 0),
		big.NewInt(10),
		common.GetTimepoint(),
	)
	if err != nil {
		t.Fatalf("Expected to be able to deposit different token")
	}
}

func TestCalculateNewGasPrice(t *testing.T) {
	initPrice := common.GweiToWei(1)
	newPrice := calculateNewGasPrice(initPrice, 0)
	if newPrice.Cmp(newPrice) != 0 {
		t.Errorf("new price is not equal to initial price with count == 0")
	}

	prevPrice := initPrice
	for count := uint64(1); count < 10; count++ {
		newPrice = calculateNewGasPrice(initPrice, count)
		if newPrice.Cmp(prevPrice) != 1 {
			t.Errorf("new price %s is not higher than previous price %s",
				newPrice.String(),
				prevPrice.String())
		}
		t.Logf("new price: %s", newPrice.String())
		prevPrice = newPrice
	}
}
