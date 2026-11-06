// API types. The wire types are generated from api/openapi.yaml into ./gen
// (run `make openapi`); this module re-exports them and adds frontend-only types.
import { zCompanySize, zLeadSort, zLeadStatus } from './gen/zod.gen.js';
import type { LeadStatus, LeadSort } from './gen/types.gen.js';

export type {
	CompanySize,
	Lead,
	LeadCreate,
	LeadDetail,
	LeadList,
	LeadPatch,
	LeadSort,
	LeadStatus,
	Tier
} from './gen/types.gen.js';

// Enum values, derived from the generated zod enums (and so from the spec).
export const LEAD_STATUSES = zLeadStatus.options;
export const COMPANY_SIZES = zCompanySize.options;
export const LEAD_SORTS = zLeadSort.unwrap().options;

/** Lead list filters as held in the URL (frontend-only). */
export interface LeadFilters {
	status: LeadStatus | null;
	q: string;
	sort: LeadSort;
	limit: number;
	offset: number;
}
