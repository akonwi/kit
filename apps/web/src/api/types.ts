// Names for the generated wire records the client uses. The client declares
// no request, response, or event records of its own (ADR 0059).
import type { components, paths } from "./schema";

type Schemas = components["schemas"];

export type Health = Schemas["Health"];
export type DeviceInfo = Schemas["DeviceInfo"];
export type RedeemPairingResult = Schemas["RedeemPairingResult"];
export type SessionInfo = Schemas["SessionInfo"];
export type SessionList = Schemas["SessionList"];
export type SessionActivityEntry = Schemas["SessionActivityEntry"];
export type SessionSnapshot = Schemas["SessionSnapshot"];
export type SessionEvent = Schemas["SessionEvent"];
export type SessionEventBatch = Schemas["SessionEventBatch"];
export type TranscriptMessage = Schemas["TranscriptMessage"];
export type TranscriptContent = Schemas["TranscriptContent"];
export type TranscriptPage = Schemas["TranscriptPage"];
export type InteractionRequest = Schemas["InteractionRequest"];
export type InteractionResponse = Schemas["InteractionResponse"];
export type FollowUpQueue = Schemas["FollowUpQueue"];
export type PromptInput = Schemas["PromptInput"];
export type PromptSubmission = Schemas["PromptSubmission"];
export type ConfigureSessionResult = Schemas["ConfigureSessionResult"];
export type ModelCapability = Schemas["ModelCapability"];
export type RestoreFollowUpsResult = Schemas["RestoreFollowUpsResult"];
export type PromoteFollowUpsResult = Schemas["PromoteFollowUpsResult"];
export type SessionUsage = Schemas["SessionUsage"];
export type CreateSessionInput = Schemas["CreateSessionInput"];
export type ForkSessionResult = Schemas["ForkSessionResult"];
export type Highlight = Schemas["Highlight"];
export type HighlightCodeBlock = Schemas["HighlightCodeBlock"];
export type HighlightCodeResult = Schemas["HighlightCodeResult"];
export type CompactSessionResult = Schemas["CompactSessionResult"];
export type CompactionFailureReason = Schemas["CompactionFailedErrorDetails"]["reason"];
export type ServerSettings = Schemas["ServerSettings"];
export type AttachmentInfo = Schemas["AttachmentInfo"];
export type AttachmentResolution = Schemas["AttachmentResolution"];
export type MessagePage = Schemas["MessagePage"];
export type BashExecution = Schemas["BashExecution"];
export type BashExecutionInput = Schemas["BashExecutionInput"];
export type BashAbortResult = Schemas["BashAbortResult"];
export type WorkspaceRef = Schemas["WorkspaceRef"];
export type Annotation = Schemas["Annotation"];
export type AnnotationAnchor = Schemas["AnnotationAnchor"];
export type AnnotationPage = Schemas["AnnotationPage"];
export type AnnotationSummary = Schemas["AnnotationSummary"];
export type SubmittedAnnotation = Schemas["SubmittedAnnotation"];
export type DiffAnchor = Schemas["WorkingTreeDiffAnnotationAnchor"];
export type DiffTargetCatalog = Schemas["DiffTargetCatalog"];
export type DiffTargetEntry = Schemas["DiffTargetEntry"];
export type DiffFileSummary = Schemas["DiffFileSummary"];
export type DiffHunk = Schemas["DiffHunk"];
export type DiffLine = Schemas["DiffLine"];
export type FileDiffPage = Schemas["FileDiffPage"];
export type ObserveDiffInput = Schemas["ObserveDiffInput"];
export type ReadFileDiffInput = Schemas["ReadFileDiffInput"];
export type WorkingTreePage = Schemas["WorkingTreePage"];
export type SessionFileIndex = Schemas["SessionFileIndex"];
export type WorkspaceFileRead = Schemas["WorkspaceFileRead"];
export type ReadWorkspaceFileInput = Schemas["ReadWorkspaceFileInput"];
export type Scratchpad = Schemas["Scratchpad"];
export type UpdateScratchpadInput = Schemas["UpdateScratchpadInput"];

/** A session event of one kind. */
export type EventOf<K extends SessionEvent["kind"]> = Extract<SessionEvent, { kind: K }>;

/** The session protocol this client speaks, as the contract states it. */
export type ProtocolVersion = paths["/v1/sessions"]["get"]["parameters"]["header"]["X-Kit-Protocol-Version"];
export const PROTOCOL_VERSION: ProtocolVersion = 50;
