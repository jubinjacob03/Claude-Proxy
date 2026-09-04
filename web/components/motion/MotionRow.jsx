"use client";

import { motion } from "motion/react";

export default function MotionRow({ children, className }) {
  return (
    <motion.tr
      className={className}
      initial={{ opacity: 0, y: 12 }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true, margin: "-40px" }}
      transition={{ duration: 0.4, ease: [0.2, 0.8, 0.2, 1] }}
    >
      {children}
    </motion.tr>
  );
}
