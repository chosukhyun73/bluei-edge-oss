import type { StateVector } from '../../lib/types';
import { relativeTime } from '../../lib/format';
import { Badge } from '../ui/badge';
import { useLanguage } from '../../lib/language-context';

const deviceTypeKeys: Record<string, string> = {
  feeder: 'equipmentSection.deviceFeeder',
  aerator: 'equipmentSection.deviceAerator',
  pump: 'equipmentSection.devicePump',
  sensor: 'equipmentSection.deviceSensor',
  camera: 'equipmentSection.deviceCamera',
  light: 'equipmentSection.deviceLight',
  heater: 'equipmentSection.deviceHeater',
  filter: 'equipmentSection.deviceFilter',
};

type Props = { data?: StateVector['equipment'] };

export function EquipmentSection({ data }: Props) {
  const { tr } = useLanguage();
  if (!data) return <p className="text-sm text-muted-foreground italic">{tr('equipmentSection.noData')}</p>;
  const devices = Array.isArray(data.devices) ? data.devices : [];
  const count = typeof data.count === 'number' ? data.count : devices.length;
  const healthSummary = data.health_summary || '—';
  const isHealthy = healthSummary === 'all_healthy';
  const notes = Array.isArray(data.notes) ? data.notes : [];

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <span className="text-sm text-muted-foreground">{tr('equipmentSection.deviceCount')}</span>
        <span className="font-mono font-medium">{tr('equipmentSection.deviceCountSuffix', { count })}</span>
        <Badge
          className={`text-xs ${isHealthy ? 'bg-green-500/20 text-green-400 border-green-500/30' : 'bg-red-500/20 text-red-400 border-red-500/30'}`}
        >
          {isHealthy ? tr('equipmentSection.allHealthy') : healthSummary}
        </Badge>
      </div>

      {devices.length > 0 && (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
          {devices.map(device => {
            const statusColor =
              device.status === 'online'
                ? 'bg-green-500/20 text-green-400 border-green-500/30'
                : device.status === 'offline'
                ? 'bg-red-500/20 text-red-400 border-red-500/30'
                : 'bg-gray-500/20 text-gray-400 border-gray-500/30';

            return (
              <div key={device.device_id} className="bg-muted/40 rounded-lg p-3 flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <div className="text-sm font-medium">{deviceTypeKeys[device.device_type] ? tr(deviceTypeKeys[device.device_type]) : device.device_type}</div>
                  <div className="text-xs text-muted-foreground font-mono truncate">{device.device_id}</div>
                  <div className="text-xs text-muted-foreground mt-0.5">{relativeTime(device.last_seen_at)}</div>
                </div>
                <Badge className={`text-xs flex-shrink-0 ${statusColor}`}>{device.status}</Badge>
              </div>
            );
          })}
        </div>
      )}

      {notes.length > 0 && (
        <ul className="space-y-0.5">
          {notes.map((n, i) => (
            <li key={i} className="text-xs text-muted-foreground italic">{n}</li>
          ))}
        </ul>
      )}
    </div>
  );
}
