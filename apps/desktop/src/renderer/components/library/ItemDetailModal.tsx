import { useKnowledgeStore } from "../../stores/knowledge.store";
import { Modal } from "../ui/Modal";
import { ReviewNavigation } from "./LibraryWorkflow";
import { ItemDetail } from "./ItemDetail";

/**
 * 列表视图下的详情浮层。表格占满内容区后没有常驻详情栏，
 * 点击行改为在浮层里打开同一套详情 UI（对齐 PromptHub 的交互）。
 */
export function ItemDetailModal({
  isOpen,
  onClose,
}: {
  isOpen: boolean;
  onClose: () => void;
}) {
  const selectedId = useKnowledgeStore((state) => state.selectedId);
  const flushPendingSave = useKnowledgeStore(
    (state) => state.flushPendingSave,
  );

  // 详情异步加载，加载完成前不开浮层，避免闪一下上一条的内容
  const isReady = Boolean(selectedId);

  const handleClose = async () => {
    if (await flushPendingSave()) onClose();
  };

  return (
    <Modal
      isOpen={isOpen && isReady}
      onClose={handleClose}
      size="fullscreen"
      contentClassName="flex flex-col overflow-hidden"
    >
      <ReviewNavigation />
      <div className="min-h-0 flex-1"><ItemDetail onClose={handleClose} /></div>
    </Modal>
  );
}
