import type {
    AIRouteBatchStatus,
    GenerateAIRouteCurrentBatch,
    GenerateAIRouteRunningBatch,
} from './group';

export function normalizeAIRouteCurrentBatch(
    batch: Omit<GenerateAIRouteCurrentBatch, 'model_count'> & { model_count?: number },
): GenerateAIRouteCurrentBatch {
    return {
        ...batch,
        index: typeof batch.index === 'number' ? batch.index : 0,
        total: typeof batch.total === 'number' ? batch.total : 0,
        model_count: typeof batch.model_count === 'number' ? batch.model_count : 0,
        channel_ids: Array.isArray(batch.channel_ids) ? batch.channel_ids : [],
        channel_names: Array.isArray(batch.channel_names) ? batch.channel_names : [],
        service_name: typeof batch.service_name === 'string' ? batch.service_name : undefined,
        attempt: typeof batch.attempt === 'number' ? batch.attempt : 0,
        status: typeof batch.status === 'string' ? batch.status : undefined,
        message: typeof batch.message === 'string' ? batch.message : undefined,
    };
}

export function normalizeAIRouteRunningBatch(
    batch: Omit<GenerateAIRouteRunningBatch, 'model_count'> & { model_count?: number },
): GenerateAIRouteRunningBatch {
    return {
        ...batch,
        index: typeof batch.index === 'number' ? batch.index : 0,
        total: typeof batch.total === 'number' ? batch.total : 0,
        model_count: typeof batch.model_count === 'number' ? batch.model_count : 0,
        channel_ids: Array.isArray(batch.channel_ids) ? batch.channel_ids : [],
        channel_names: Array.isArray(batch.channel_names) ? batch.channel_names : [],
        service_name: typeof batch.service_name === 'string' ? batch.service_name : undefined,
        attempt: typeof batch.attempt === 'number' ? batch.attempt : 0,
        status: typeof batch.status === 'string' ? batch.status as AIRouteBatchStatus : undefined,
        message: typeof batch.message === 'string' ? batch.message : undefined,
    };
}
