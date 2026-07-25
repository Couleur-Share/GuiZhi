import { useKnowledgeStore } from "../../stores/knowledge.store";
import { Modal } from "../ui/Modal";
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
  const item = useKnowledgeStore((state) => state.selectedItem);
  const flushPendingSave = useKnowledgeStore(
    (state) => state.flushPendingSave,
  );

  // 详情异步加载，加载完成前不开浮层，避免闪一下上一条的内容
  const isReady = item !== null && item.id === selectedId;

  const handleClose = () => {
    void flushPendingSave();
    onClose();
  };

  return (
    <Modal
      isOpen={isOpen && isReady}
      onClose={handleClose}
      size="fullscreen"
      contentClassName="overflow-hidden"
    >
      <ItemDetail onClose={handleClose} />
    </Modal>
  );
}
