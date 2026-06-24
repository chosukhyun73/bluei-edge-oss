import { Card, CardContent } from '../ui/card';
import { useLanguage } from '../../lib/language-context';

/**
 * 5/9 ai-training.html 마지막 섹션 — 안전 안내 (항상 노출).
 */
export function SafetyInfoCard() {
  const { tr } = useLanguage();
  return (
    <Card>
      <CardContent className="pt-5">
        <div className="space-y-2">
          <p className="text-sm font-semibold text-blue-300">{tr('safetyInfoCard.title')}</p>
          <ul className="list-disc list-inside text-xs text-gray-300 space-y-1 pl-1">
            <li>
              {tr('safetyInfoCard.item1Pre')}<b>{tr('safetyInfoCard.item1Bold')}</b>{tr('safetyInfoCard.item1Post')}
            </li>
            <li>
              {tr('safetyInfoCard.item2Pre')}<b>{tr('safetyInfoCard.item2Bold')}</b>
            </li>
            <li>
              {tr('safetyInfoCard.item3Pre')}<b>{tr('safetyInfoCard.item3Bold')}</b>{tr('safetyInfoCard.item3Post')}
            </li>
            <li>{tr('safetyInfoCard.item4')}</li>
          </ul>
        </div>
      </CardContent>
    </Card>
  );
}
