package functions_def

type FunctionName string

const (
	Clusterize              FunctionName = "clusterize"
	ClusterizeFinalizaition FunctionName = "clusterize_finalization"
	Deploy                  FunctionName = "deploy"
	Protect                 FunctionName = "protect"
	Report                  FunctionName = "report"
	Join                    FunctionName = "join"
	JoinFinalization        FunctionName = "join_finalization"
	Status                  FunctionName = "status"
)

type FunctionDef interface {
	GetFunctionCmdDefinition(name FunctionName) string
}
