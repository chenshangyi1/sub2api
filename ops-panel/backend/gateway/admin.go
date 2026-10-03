// 管理面入口：AdminService 嵌入 *Service 共享依赖。
// 对外仍通过 Service 委托；包文档见 service.go。
package gateway

// AdminService 网关管理面。
// 嵌入 *Service 以共享仓储、配置与运行时辅助方法；对外仍通过 Service 薄委托保持 API 兼容。
type AdminService struct {
	*Service
}

// admin 返回管理面入口；兼容测试中未走 NewService 的 *Service 字面量。
func (s *Service) admin() *AdminService {
	if s == nil {
		return nil
	}
	if s.Admin == nil {
		s.Admin = &AdminService{Service: s}
	} else if s.Admin.Service == nil {
		s.Admin.Service = s
	}
	return s.Admin
}
