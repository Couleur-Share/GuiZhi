package services

// Logger 把原生界面里的失败写进诊断日志（error.log）。
// 字段截断与家目录脱敏由实现负责（适配器复用渲染进程用的同一个 log:appError 处理器，
// 所以两套界面的日志格式、脱敏规则、轮转是同一份）；界面只管给出范围、动作与原因。
//
// 后台任务的失败没有用户在看，必须经它留痕；用户操作的失败既弹提示也走它。
//
// Logger writes UI-side failures to the diagnostic error log.
type Logger interface {
	AppError(scope, action, message string)
}
