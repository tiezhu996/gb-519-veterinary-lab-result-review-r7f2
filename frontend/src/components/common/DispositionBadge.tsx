import { dispositionLabel, dispositionTone } from '../../utils/disposition';

// DispositionBadge 是检测页与结果页共用的危急处置状态徽标。
export function DispositionBadge({ status }: { status?: string | null }) {
  return <span className={`status status--${dispositionTone(status)}`}>{dispositionLabel(status)}</span>;
}
