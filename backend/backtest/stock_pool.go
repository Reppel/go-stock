package backtest

import (
	"go-stock/backend/data"
	"go-stock/backend/db"
	"go-stock/backend/models"
	"strings"
)

// StockPoolService 股票池服务
type StockPoolService struct{}

// NewStockPoolService 创建服务
func NewStockPoolService() *StockPoolService {
	return &StockPoolService{}
}

// GetStockPool 获取股票池
func (s *StockPoolService) GetStockPool(scope string) []string {
	switch scope {
	case "全部A股":
		return s.getAllAStockCodes()
	case "全部":
		return s.getAllAStockCodes()
	case "自选股":
		return s.getFollowedStockCodes()
	default:
		// 单个股票，例如 stock_000001
		if strings.HasPrefix(scope, "stock_") {
			stockCode := strings.TrimSpace(strings.TrimPrefix(scope, "stock_"))
			if stockCode == "" {
				return nil
			}
			return []string{stockCode}
		}
		// 分组名
		if strings.HasPrefix(scope, "group_") {
			groupID := strings.TrimPrefix(scope, "group_")
			return s.getGroupStockCodes(groupID)
		}
		return s.getAllAStockCodes()
	}
}

func isAllStockScope(scope string) bool {
	return scope == "" || scope == "全部A股" || scope == "全部"
}

// getAllAStockCodes 获取全部 A 股代码
func (s *StockPoolService) getAllAStockCodes() []string {
	var stocks []models.AllStockInfo
	db.Dao.Model(&models.AllStockInfo{}).Find(&stocks)

	var codes []string
	for _, s := range stocks {
		if s.SECUCODE == "" {
			continue
		}
		codes = append(codes, s.SECUCODE)
	}
	return codes
}

// getFollowedStockCodes 获取自选股代码
func (s *StockPoolService) getFollowedStockCodes() []string {
	api := data.NewStockDataApi()
	followList := api.GetFollowList(0)
	if followList == nil {
		return nil
	}

	var codes []string
	for _, stock := range *followList {
		if stock.StockCode == "" {
			continue
		}
		codes = append(codes, stock.StockCode)
	}
	return codes
}

// getGroupStockCodes 获取分组股票代码
func (s *StockPoolService) getGroupStockCodes(groupID string) []string {
	// MVP 简化：返回自选股
	return s.getFollowedStockCodes()
}
