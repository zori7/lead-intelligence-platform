from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Step(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    STEP_UNSPECIFIED: _ClassVar[Step]
    STEP_VALIDATE_INPUT: _ClassVar[Step]
    STEP_EXTRACT_COMPANY: _ClassVar[Step]
    STEP_ANALYZE_FIT: _ClassVar[Step]
    STEP_ANALYZE_INTENT: _ClassVar[Step]
    STEP_SCORE_LEAD: _ClassVar[Step]
    STEP_GENERATE_RECOMMENDATIONS: _ClassVar[Step]
    STEP_AGGREGATE: _ClassVar[Step]
    STEP_FETCH_WEBSITE: _ClassVar[Step]

class StepState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    STEP_STATE_UNSPECIFIED: _ClassVar[StepState]
    STEP_STATE_RUNNING: _ClassVar[StepState]
    STEP_STATE_DONE: _ClassVar[StepState]
    STEP_STATE_SKIPPED: _ClassVar[StepState]
    STEP_STATE_FAILED: _ClassVar[StepState]

class ErrorCode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ERROR_CODE_UNSPECIFIED: _ClassVar[ErrorCode]
    ERROR_CODE_INVALID_INPUT: _ClassVar[ErrorCode]
    ERROR_CODE_INSUFFICIENT_DATA: _ClassVar[ErrorCode]
    ERROR_CODE_PROVIDER_ERROR: _ClassVar[ErrorCode]
    ERROR_CODE_PROVIDER_CONFIG: _ClassVar[ErrorCode]
    ERROR_CODE_INTERNAL: _ClassVar[ErrorCode]

class Tier(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    TIER_UNSPECIFIED: _ClassVar[Tier]
    TIER_HOT: _ClassVar[Tier]
    TIER_WARM: _ClassVar[Tier]
    TIER_COLD: _ClassVar[Tier]

class Priority(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PRIORITY_UNSPECIFIED: _ClassVar[Priority]
    PRIORITY_HIGH: _ClassVar[Priority]
    PRIORITY_MEDIUM: _ClassVar[Priority]
    PRIORITY_LOW: _ClassVar[Priority]

class WebsiteFetchStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    WEBSITE_FETCH_STATUS_UNSPECIFIED: _ClassVar[WebsiteFetchStatus]
    WEBSITE_FETCH_STATUS_FETCHED: _ClassVar[WebsiteFetchStatus]
    WEBSITE_FETCH_STATUS_NOT_PROVIDED: _ClassVar[WebsiteFetchStatus]
    WEBSITE_FETCH_STATUS_DISABLED: _ClassVar[WebsiteFetchStatus]
    WEBSITE_FETCH_STATUS_BLOCKED: _ClassVar[WebsiteFetchStatus]
    WEBSITE_FETCH_STATUS_FAILED: _ClassVar[WebsiteFetchStatus]
STEP_UNSPECIFIED: Step
STEP_VALIDATE_INPUT: Step
STEP_EXTRACT_COMPANY: Step
STEP_ANALYZE_FIT: Step
STEP_ANALYZE_INTENT: Step
STEP_SCORE_LEAD: Step
STEP_GENERATE_RECOMMENDATIONS: Step
STEP_AGGREGATE: Step
STEP_FETCH_WEBSITE: Step
STEP_STATE_UNSPECIFIED: StepState
STEP_STATE_RUNNING: StepState
STEP_STATE_DONE: StepState
STEP_STATE_SKIPPED: StepState
STEP_STATE_FAILED: StepState
ERROR_CODE_UNSPECIFIED: ErrorCode
ERROR_CODE_INVALID_INPUT: ErrorCode
ERROR_CODE_INSUFFICIENT_DATA: ErrorCode
ERROR_CODE_PROVIDER_ERROR: ErrorCode
ERROR_CODE_PROVIDER_CONFIG: ErrorCode
ERROR_CODE_INTERNAL: ErrorCode
TIER_UNSPECIFIED: Tier
TIER_HOT: Tier
TIER_WARM: Tier
TIER_COLD: Tier
PRIORITY_UNSPECIFIED: Priority
PRIORITY_HIGH: Priority
PRIORITY_MEDIUM: Priority
PRIORITY_LOW: Priority
WEBSITE_FETCH_STATUS_UNSPECIFIED: WebsiteFetchStatus
WEBSITE_FETCH_STATUS_FETCHED: WebsiteFetchStatus
WEBSITE_FETCH_STATUS_NOT_PROVIDED: WebsiteFetchStatus
WEBSITE_FETCH_STATUS_DISABLED: WebsiteFetchStatus
WEBSITE_FETCH_STATUS_BLOCKED: WebsiteFetchStatus
WEBSITE_FETCH_STATUS_FAILED: WebsiteFetchStatus

class AnalyzeLeadRequest(_message.Message):
    __slots__ = ("job_id", "lead", "models")
    JOB_ID_FIELD_NUMBER: _ClassVar[int]
    LEAD_FIELD_NUMBER: _ClassVar[int]
    MODELS_FIELD_NUMBER: _ClassVar[int]
    job_id: str
    lead: LeadInput
    models: _containers.RepeatedCompositeFieldContainer[NodeModelConfig]
    def __init__(self, job_id: _Optional[str] = ..., lead: _Optional[_Union[LeadInput, _Mapping]] = ..., models: _Optional[_Iterable[_Union[NodeModelConfig, _Mapping]]] = ...) -> None: ...

class NodeModelConfig(_message.Message):
    __slots__ = ("step", "model", "temperature")
    STEP_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    TEMPERATURE_FIELD_NUMBER: _ClassVar[int]
    step: Step
    model: str
    temperature: float
    def __init__(self, step: _Optional[_Union[Step, str]] = ..., model: _Optional[str] = ..., temperature: _Optional[float] = ...) -> None: ...

class ListModelsRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListModelsResponse(_message.Message):
    __slots__ = ("models",)
    MODELS_FIELD_NUMBER: _ClassVar[int]
    models: _containers.RepeatedCompositeFieldContainer[ModelInfo]
    def __init__(self, models: _Optional[_Iterable[_Union[ModelInfo, _Mapping]]] = ...) -> None: ...

class ModelInfo(_message.Message):
    __slots__ = ("id",)
    ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    def __init__(self, id: _Optional[str] = ...) -> None: ...

class LeadInput(_message.Message):
    __slots__ = ("company_name", "website", "contact_name", "contact_title", "industry", "company_size", "notes")
    COMPANY_NAME_FIELD_NUMBER: _ClassVar[int]
    WEBSITE_FIELD_NUMBER: _ClassVar[int]
    CONTACT_NAME_FIELD_NUMBER: _ClassVar[int]
    CONTACT_TITLE_FIELD_NUMBER: _ClassVar[int]
    INDUSTRY_FIELD_NUMBER: _ClassVar[int]
    COMPANY_SIZE_FIELD_NUMBER: _ClassVar[int]
    NOTES_FIELD_NUMBER: _ClassVar[int]
    company_name: str
    website: str
    contact_name: str
    contact_title: str
    industry: str
    company_size: str
    notes: str
    def __init__(self, company_name: _Optional[str] = ..., website: _Optional[str] = ..., contact_name: _Optional[str] = ..., contact_title: _Optional[str] = ..., industry: _Optional[str] = ..., company_size: _Optional[str] = ..., notes: _Optional[str] = ...) -> None: ...

class AnalysisEvent(_message.Message):
    __slots__ = ("job_id", "sequence", "progress", "completed", "failed")
    JOB_ID_FIELD_NUMBER: _ClassVar[int]
    SEQUENCE_FIELD_NUMBER: _ClassVar[int]
    PROGRESS_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_FIELD_NUMBER: _ClassVar[int]
    FAILED_FIELD_NUMBER: _ClassVar[int]
    job_id: str
    sequence: int
    progress: StepProgress
    completed: AnalysisCompleted
    failed: AnalysisFailed
    def __init__(self, job_id: _Optional[str] = ..., sequence: _Optional[int] = ..., progress: _Optional[_Union[StepProgress, _Mapping]] = ..., completed: _Optional[_Union[AnalysisCompleted, _Mapping]] = ..., failed: _Optional[_Union[AnalysisFailed, _Mapping]] = ...) -> None: ...

class StepProgress(_message.Message):
    __slots__ = ("step", "state", "percent", "message")
    STEP_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    PERCENT_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    step: Step
    state: StepState
    percent: int
    message: str
    def __init__(self, step: _Optional[_Union[Step, str]] = ..., state: _Optional[_Union[StepState, str]] = ..., percent: _Optional[int] = ..., message: _Optional[str] = ...) -> None: ...

class AnalysisCompleted(_message.Message):
    __slots__ = ("result",)
    RESULT_FIELD_NUMBER: _ClassVar[int]
    result: AnalysisResult
    def __init__(self, result: _Optional[_Union[AnalysisResult, _Mapping]] = ...) -> None: ...

class AnalysisFailed(_message.Message):
    __slots__ = ("code", "message", "retryable")
    CODE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    RETRYABLE_FIELD_NUMBER: _ClassVar[int]
    code: ErrorCode
    message: str
    retryable: bool
    def __init__(self, code: _Optional[_Union[ErrorCode, str]] = ..., message: _Optional[str] = ..., retryable: _Optional[bool] = ...) -> None: ...

class AnalysisResult(_message.Message):
    __slots__ = ("summary", "company", "fit", "intent", "score", "recommendations", "meta")
    SUMMARY_FIELD_NUMBER: _ClassVar[int]
    COMPANY_FIELD_NUMBER: _ClassVar[int]
    FIT_FIELD_NUMBER: _ClassVar[int]
    INTENT_FIELD_NUMBER: _ClassVar[int]
    SCORE_FIELD_NUMBER: _ClassVar[int]
    RECOMMENDATIONS_FIELD_NUMBER: _ClassVar[int]
    META_FIELD_NUMBER: _ClassVar[int]
    summary: str
    company: CompanyInsights
    fit: FitAnalysis
    intent: IntentAnalysis
    score: LeadScore
    recommendations: _containers.RepeatedCompositeFieldContainer[Recommendation]
    meta: ResultMeta
    def __init__(self, summary: _Optional[str] = ..., company: _Optional[_Union[CompanyInsights, _Mapping]] = ..., fit: _Optional[_Union[FitAnalysis, _Mapping]] = ..., intent: _Optional[_Union[IntentAnalysis, _Mapping]] = ..., score: _Optional[_Union[LeadScore, _Mapping]] = ..., recommendations: _Optional[_Iterable[_Union[Recommendation, _Mapping]]] = ..., meta: _Optional[_Union[ResultMeta, _Mapping]] = ...) -> None: ...

class CompanyInsights(_message.Message):
    __slots__ = ("name", "industry", "size_estimate", "business_model", "description", "key_needs", "confidence")
    NAME_FIELD_NUMBER: _ClassVar[int]
    INDUSTRY_FIELD_NUMBER: _ClassVar[int]
    SIZE_ESTIMATE_FIELD_NUMBER: _ClassVar[int]
    BUSINESS_MODEL_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    KEY_NEEDS_FIELD_NUMBER: _ClassVar[int]
    CONFIDENCE_FIELD_NUMBER: _ClassVar[int]
    name: str
    industry: str
    size_estimate: str
    business_model: str
    description: str
    key_needs: _containers.RepeatedScalarFieldContainer[str]
    confidence: float
    def __init__(self, name: _Optional[str] = ..., industry: _Optional[str] = ..., size_estimate: _Optional[str] = ..., business_model: _Optional[str] = ..., description: _Optional[str] = ..., key_needs: _Optional[_Iterable[str]] = ..., confidence: _Optional[float] = ...) -> None: ...

class FitAnalysis(_message.Message):
    __slots__ = ("score", "rationale", "matched_criteria", "gaps")
    SCORE_FIELD_NUMBER: _ClassVar[int]
    RATIONALE_FIELD_NUMBER: _ClassVar[int]
    MATCHED_CRITERIA_FIELD_NUMBER: _ClassVar[int]
    GAPS_FIELD_NUMBER: _ClassVar[int]
    score: int
    rationale: str
    matched_criteria: _containers.RepeatedScalarFieldContainer[str]
    gaps: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, score: _Optional[int] = ..., rationale: _Optional[str] = ..., matched_criteria: _Optional[_Iterable[str]] = ..., gaps: _Optional[_Iterable[str]] = ...) -> None: ...

class IntentAnalysis(_message.Message):
    __slots__ = ("score", "rationale", "signals")
    SCORE_FIELD_NUMBER: _ClassVar[int]
    RATIONALE_FIELD_NUMBER: _ClassVar[int]
    SIGNALS_FIELD_NUMBER: _ClassVar[int]
    score: int
    rationale: str
    signals: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, score: _Optional[int] = ..., rationale: _Optional[str] = ..., signals: _Optional[_Iterable[str]] = ...) -> None: ...

class ScoreBreakdown(_message.Message):
    __slots__ = ("fit", "intent")
    FIT_FIELD_NUMBER: _ClassVar[int]
    INTENT_FIELD_NUMBER: _ClassVar[int]
    fit: int
    intent: int
    def __init__(self, fit: _Optional[int] = ..., intent: _Optional[int] = ...) -> None: ...

class LeadScore(_message.Message):
    __slots__ = ("value", "tier", "confidence", "breakdown")
    VALUE_FIELD_NUMBER: _ClassVar[int]
    TIER_FIELD_NUMBER: _ClassVar[int]
    CONFIDENCE_FIELD_NUMBER: _ClassVar[int]
    BREAKDOWN_FIELD_NUMBER: _ClassVar[int]
    value: int
    tier: Tier
    confidence: float
    breakdown: ScoreBreakdown
    def __init__(self, value: _Optional[int] = ..., tier: _Optional[_Union[Tier, str]] = ..., confidence: _Optional[float] = ..., breakdown: _Optional[_Union[ScoreBreakdown, _Mapping]] = ...) -> None: ...

class Recommendation(_message.Message):
    __slots__ = ("action", "rationale", "priority")
    ACTION_FIELD_NUMBER: _ClassVar[int]
    RATIONALE_FIELD_NUMBER: _ClassVar[int]
    PRIORITY_FIELD_NUMBER: _ClassVar[int]
    action: str
    rationale: str
    priority: Priority
    def __init__(self, action: _Optional[str] = ..., rationale: _Optional[str] = ..., priority: _Optional[_Union[Priority, str]] = ...) -> None: ...

class ResultMeta(_message.Message):
    __slots__ = ("node_models", "partial", "warnings", "duration_ms", "input_tokens", "output_tokens", "website")
    class NodeModelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NODE_MODELS_FIELD_NUMBER: _ClassVar[int]
    PARTIAL_FIELD_NUMBER: _ClassVar[int]
    WARNINGS_FIELD_NUMBER: _ClassVar[int]
    DURATION_MS_FIELD_NUMBER: _ClassVar[int]
    INPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    WEBSITE_FIELD_NUMBER: _ClassVar[int]
    node_models: _containers.ScalarMap[str, str]
    partial: bool
    warnings: _containers.RepeatedScalarFieldContainer[str]
    duration_ms: int
    input_tokens: int
    output_tokens: int
    website: WebsiteFetch
    def __init__(self, node_models: _Optional[_Mapping[str, str]] = ..., partial: _Optional[bool] = ..., warnings: _Optional[_Iterable[str]] = ..., duration_ms: _Optional[int] = ..., input_tokens: _Optional[int] = ..., output_tokens: _Optional[int] = ..., website: _Optional[_Union[WebsiteFetch, _Mapping]] = ...) -> None: ...

class WebsiteFetch(_message.Message):
    __slots__ = ("status", "final_url")
    STATUS_FIELD_NUMBER: _ClassVar[int]
    FINAL_URL_FIELD_NUMBER: _ClassVar[int]
    status: WebsiteFetchStatus
    final_url: str
    def __init__(self, status: _Optional[_Union[WebsiteFetchStatus, str]] = ..., final_url: _Optional[str] = ...) -> None: ...
