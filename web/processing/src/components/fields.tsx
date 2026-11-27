// 라벨이 위에 붙는 입력 칸.
const INPUT_CLASS = 'h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-green-500/50';

export function Field({ label, value, onChange, type = 'text' }: {
  label: string; value: string; onChange: (v: string) => void;
  type?: 'text' | 'number' | 'password' | 'date';
}) {
  return (
    <div className="flex flex-col gap-1">
      <label className="text-xs text-gray-400 font-medium">{label}</label>
      <input type={type} value={value} onChange={e => onChange(e.target.value)} className={INPUT_CLASS} />
    </div>
  );
}

export function SelectField({ label, value, onChange, options }: {
  label: string; value: string; onChange: (v: string) => void;
  options: { value: string; label: string }[];
}) {
  return (
    <div className="flex flex-col gap-1">
      <label className="text-xs text-gray-400 font-medium">{label}</label>
      <select value={value} onChange={e => onChange(e.target.value)} className={INPUT_CLASS}>
        {options.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
      </select>
    </div>
  );
}
