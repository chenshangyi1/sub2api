// 数据面入口：Runtime 嵌入 *Service 共享依赖。
// 对外仍通过 Service 委托；包文档见 service.go。
package gateway

// Runtime 网关数据面运行时。
// 嵌入 *Service 以共享仓储、配置、缓存与依赖。
type Runtime struct {
	*Service
}

// runtime 返回数据面入口；兼容测试中未走 NewService 的 *Service 字面量。
func (s *Service) runtime() *Runtime {
	if s == nil {
		return nil
	}
	if s.Runtime == nil {
		s.Runtime = &Runtime{Service: s}
	} else if s.Runtime.Service == nil {
		s.Runtime.Service = s
	}
	return s.Runtime
}
