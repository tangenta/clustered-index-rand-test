package sqlgen

type State struct {
	ctrl *ControlOption

	tables           []*Table
	scope            []map[ScopeKeyType]ScopeObj
	enabledClustered bool

	prepareStmts []*Prepare

	finishInit bool
	todoSQLs   []string
	weight     *Weight
}

func (s *State) WithWeight(w *Weight) {
	s.weight = w
}

var DefaultWeight = Weight{
	CreateTable:                 13,
	CreateTable_MoreCol:         2,
	CreateTable_IndexMoreCol:    2,
	CreateTable_MustPrefixIndex: false,
	CreateTable_WithoutLike:     4,
	CreateTable_Partition_Type:  "",
	CreateTable_MustStrCol:      false,
	CreateTable_MustIntCol:      false,
	Query:                       15,
	Query_DML:                   20,
	Query_Select:                1,
	Query_DML_DEL:               1,
	Query_DML_UPDATE:            1,
	Query_DML_INSERT:            1,
	Query_DML_INSERT_ON_DUP:     4,
	Query_DML_Can_Be_Replace:    true,
	Query_DDL:                   5,
	Query_Split:                 0,
	Query_Analyze:               0,
	Query_Prepare:               2,
	Query_HasLimit:              1,
}

type Weight struct {
	CreateTable                 int
	CreateTable_MoreCol         int
	CreateTable_WithoutLike     int
	CreateTable_Partition_Type  string
	CreateTable_IndexMoreCol    int
	CreateTable_MustPrefixIndex bool
	CreateTable_MustStrCol      bool
	CreateTable_MustIntCol      bool
	Query                       int
	Query_DML                   int
	Query_Select                int
	Query_DML_DEL               int
	Query_DML_INSERT            int
	Query_DML_INSERT_ON_DUP     int
	Query_DML_Can_Be_Replace    bool
	Query_DML_UPDATE            int
	Query_DDL                   int
	Query_Split                 int
	Query_Analyze               int
	Query_Prepare               int
	Query_HasLimit              int
}

type Table struct {
	Id      int
	Name    string
	Columns []*Column
	Indices []*Index

	containsPK       bool // to ensure at most 1 pk in each table
	HandleCols       []*Column
	PartitionColumns []*Column
	values           [][]string

	// childTables records tables that have the same structure.
	// A table is also its childTables.
	// This is used for SELECT OUT FILE and LOAD DATA.
	childTables []*Table
}

type Column struct {
	Id   int
	Name string
	Tp   ColumnType

	isUnsigned bool
	arg1       int      // optional
	arg2       int      // optional
	args       []string // for ColumnTypeSet and ColumnTypeEnum

	defaultVal     string
	isNotNull      bool
	relatedIndices map[int]struct{}
}

type Index struct {
	Id           int
	Name         string
	Tp           IndexType
	Columns      []*Column
	ColumnPrefix []int
}

type Prepare struct {
	Id   int
	Name string
	Args []func() string
}

func NewState() *State {
	s := &State{
		ctrl:             DefaultControlOption(),
		enabledClustered: true,
	}
	s.CreateScope()
	return s
}

func NewState2(EnableTestTiFlash bool) *State {
	s := &State{
		ctrl:             DefaultControlOption(),
		enabledClustered: true,
	}
	s.ctrl.EnableTestTiFlash = EnableTestTiFlash
	s.CreateScope()
	return s
}

type ControlOption struct {
	// the initial number of tables.
	InitTableCount int
	// the number of rows to initialize for each table.
	InitRowCount int
	// the number of Columns for each tables.
	InitColCount int

	// the max number of tables.
	MaxTableNum int
	// for the Columns that have no default value,
	// whether allow to omit column names in 'INSERT' statement.
	StrictTransTable bool
	// indicate that the testing server has gc save point.
	CanReadGCSavePoint bool
	// Test SELECT OUTFILE and LOAD DATA
	EnableSelectOutFileAndLoadData bool
	// Test TiFlash
	EnableTestTiFlash bool
}

func DefaultControlOption() *ControlOption {
	return &ControlOption{
		InitTableCount:                 5,
		InitRowCount:                   10,
		InitColCount:                   5,
		MaxTableNum:                    7,
		StrictTransTable:               true,
		CanReadGCSavePoint:             false,
		EnableSelectOutFileAndLoadData: false,
		EnableTestTiFlash:              false,
	}
}

type ScopeObj struct {
	obj interface{}
}

func NewScopeObj(obj interface{}) ScopeObj {
	return ScopeObj{obj}
}

func (s ScopeObj) IsNil() bool {
	return s.obj == nil
}

func (s ScopeObj) ToTable() *Table {
	return s.obj.(*Table)
}

func (s ScopeObj) ToTables() []*Table {
	return s.obj.([]*Table)
}

func (s ScopeObj) ToColumn() *Column {
	return s.obj.(*Column)
}

func (s ScopeObj) ToIndex() *Index {
	return s.obj.(*Index)
}

func (s ScopeObj) ToInt() int {
	return s.obj.(int)
}

func (s ScopeObj) ToColumns() []*Column {
	return s.obj.([]*Column)
}

func (s ScopeObj) ToPrepare() *Prepare {
	return s.obj.(*Prepare)
}

func (s *State) CreateScope() {
	s.scope = append(s.scope, make(map[ScopeKeyType]ScopeObj))
}

func (s *State) DestroyScope() {
	if len(s.scope) == 0 {
		return
	}
	s.scope = s.scope[:len(s.scope)-1]
}

func (s *State) Store(key ScopeKeyType, val ScopeObj) {
	Assert(!val.IsNil(), "storing a nil object")
	current := s.scope[len(s.scope)-1]
	current[key] = val
}

func (s *State) StoreInParent(key ScopeKeyType, val ScopeObj) {
	Assert(len(s.scope) > 1, "cannot StoreInParent in the root scope")
	Assert(!val.IsNil(), "storing a nil object")
	current := s.scope[len(s.scope)-2]
	current[key] = val
}

func (s *State) StoreInRoot(key ScopeKeyType, val ScopeObj) {
	s.scope[0][key] = val
}

func (s *State) Search(key ScopeKeyType) ScopeObj {
	for i := len(s.scope) - 1; i >= 0; i-- {
		current := s.scope[i]
		if v, ok := current[key]; ok {
			return v
		}
	}
	return ScopeObj{}
}

func (s *State) CreateScopeAndStore(key ScopeKeyType, val ScopeObj) {
	s.CreateScope()
	s.Store(key, val)
}

func (s *State) AllocGlobalID(key ScopeKeyType) int {
	var result int

	if v, ok := s.scope[0][key]; ok {
		result = v.ToInt()
	} else {
		result = 0
	}
	s.scope[0][key] = NewScopeObj(result + 1)
	return result
}

type ScopeListener struct {
	state *State
}

func (s *ScopeListener) BeforeProductionGen(fn *Fn) {
	s.state.CreateScope()
}

func (s *ScopeListener) AfterProductionGen(fn *Fn, result *Result) {
	s.state.DestroyScope()
}

func (s *ScopeListener) ProductionCancel(fn *Fn) {
	return
}

type PostListener struct {
	callbacks map[string]func()
}

func (p *PostListener) BeforeProductionGen(fn *Fn) {}

func (p *PostListener) AfterProductionGen(fn *Fn, result *Result) {
	if f, ok := p.callbacks[fn.Name]; ok {
		f()
		delete(p.callbacks, fn.Name)
	}
}

func (p *PostListener) ProductionCancel(fn *Fn) {}

func (p *PostListener) Register(fnName string, fn func()) {
	p.callbacks[fnName] = fn
}

type DebugListener struct {
	parentsFn      []string
	obtainedResult []*Result
}

func (d *DebugListener) BeforeProductionGen(fn *Fn) {
	d.parentsFn = append(d.parentsFn, fn.Name)
}

func (d *DebugListener) AfterProductionGen(fn *Fn, result *Result) {
	d.parentsFn = d.parentsFn[:len(d.parentsFn)-1]
	d.obtainedResult = append(d.obtainedResult, result)
}

func (d *DebugListener) ProductionCancel(fn *Fn) {
}
